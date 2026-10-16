package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// AuthenticatedGroup is the group the API server adds to every authenticated
// user; a review without it would deny what a binding to that group grants.
const AuthenticatedGroup = "system:authenticated"

// reservedPrefix starts the users and groups Kubernetes itself assigns:
// ServiceAccounts, nodes, system:masters. A caller's identity never maps to
// one, so a caller cannot be reviewed as the controller's ServiceAccount or as
// a cluster administrator.
const reservedPrefix = "system:"

// ErrReviewUnavailable wraps a check the authorizer could not decide because
// the API server did not answer it: not a refusal, so a service reports it as
// unavailable rather than as permission denied.
var ErrReviewUnavailable = errors.New("the authorization review is unavailable")

// reviewTimeout bounds one SubjectAccessReview, so that an API server that
// stops answering shows as unavailable within seconds.
const reviewTimeout = 5 * time.Second

// sessionsResource is the virtual resource a new Session is created as. A
// Session is not a Kubernetes object, so RBAC names it the way a ClusterRole
// names the API objects beside it: `sessions` in the kagent API group.
var sessionsResource = schema.GroupResource{Group: v1alpha3.GroupVersion.Group, Resource: "sessions"}

// kubernetesResources are the resource types that are Kubernetes objects,
// reviewed as the API server would review the same object.
var kubernetesResources = map[string]schema.GroupResource{
	"Agent":           {Group: v1alpha3.GroupVersion.Group, Resource: "agents"},
	"AgentTemplate":   {Group: v1alpha3.GroupVersion.Group, Resource: "agenttemplates"},
	"Harness":         {Group: v1alpha3.GroupVersion.Group, Resource: "harnesses"},
	"ModelConfig":     {Group: v1alpha3.GroupVersion.Group, Resource: "modelconfigs"},
	"SandboxTemplate": {Group: v1alpha3.GroupVersion.Group, Resource: "sandboxtemplates"},
	// The tool service lists MCPServers beside RemoteMCPServers; a tool server
	// is reviewed as the kagent-owned kind.
	"ToolServer":     {Group: v1alpha3.GroupVersion.Group, Resource: "remotemcpservers"},
	"PromptTemplate": {Group: corev1.GroupName, Resource: "configmaps"},
}

// permittedResources are the resource types RBAC has no object for, so the
// authorizer permits them: the control-plane records their service already
// limits to the caller's own rows or to a share (a Session once it exists, a
// Checkpoint, a ScheduledRun, a Sandbox), and the reads that name no
// Kubernetes object (the tool server types, the Substrate inventory).
var permittedResources = map[string]bool{
	"Session": true, "Checkpoint": true, "ScheduledRun": true, "Sandbox": true, "ToolServerType": true, "Substrate": true,
}

// allCreatorsResource is the list of every creator's Sessions. Its check names
// no namespace and its list spans every tenant, so nobody is granted it.
const allCreatorsResource = "SessionAllCreators"

// defaultDecisionTTL is how long a review's answer is reused.
const defaultDecisionTTL = 10 * time.Second

// scopeReviewConcurrency bounds the reviews a collection scope sends at once.
const scopeReviewConcurrency = 8

// SubjectAccessReviewer creates SubjectAccessReviews; client-go's typed
// client satisfies it.
type SubjectAccessReviewer interface {
	Create(context.Context, *authorizationv1.SubjectAccessReview, metav1.CreateOptions) (*authorizationv1.SubjectAccessReview, error)
}

// NamespaceLister lists Namespaces; client-go's typed client satisfies it.
type NamespaceLister interface {
	List(context.Context, metav1.ListOptions) (*corev1.NamespaceList, error)
}

// KubernetesConfig maps a principal to the subject the API server would
// authenticate from the same token, and names the namespaces a collection
// read ranges over.
//
// The username is the principal's authenticated user ID, and the groups are
// the user's groups, which the authenticator reads from the claims it is
// configured with: a person's direct calls and the calls their agent makes
// for them (which carry the user ID and the groups, and no claims) are one
// Kubernetes user. The installation points the authenticator at the claims
// the API server's --oidc-username-claim and --oidc-groups-claim name.
type KubernetesConfig struct {
	// UsernamePrefix is put before the user ID, as --oidc-username-prefix.
	UsernamePrefix string
	// GroupsPrefix is put before every group, as --oidc-groups-prefix.
	GroupsPrefix string
	// Namespaces are the namespaces a collection read ranges over. Empty
	// ranges over every Namespace that matches NamespaceSelector, which a
	// controller watching a fixed list cannot list.
	Namespaces []string
	// NamespaceSelector narrows the listed Namespaces. Nil selects every one.
	NamespaceSelector labels.Selector
	// IncludedNamespaces are ranged over beside the Namespaces that
	// NamespaceSelector selects, whatever their labels: a namespace one
	// selector cannot express beside the others, such as a shared one.
	IncludedNamespaces []string
	// DecisionTTL is how long a review's answer is reused for the same
	// subject, verb and resource. Zero selects defaultDecisionTTL.
	DecisionTTL time.Duration
}

// KubernetesAuthorizer answers with the API server's own authorization
// decisions: every check is a SubjectAccessReview for the principal mapped as
// the API server's OIDC authenticator maps the same token, so Kubernetes RBAC
// on the kagent resources is the one source of truth for what a caller sees
// and runs through the controller.
//
// A collection read is scoped to the namespaces in which the caller may list
// the resource, one review per namespace, after a cluster-wide review that
// short-circuits to the whole collection. A caller allowed nowhere gets an
// empty collection, never a refusal.
//
// Decisions are cached for DecisionTTL, so a revoked binding takes effect
// within that time. The control plane's own calls carry no principal and are
// permitted: they are the controller's, not a caller's.
type KubernetesAuthorizer struct {
	reviews    SubjectAccessReviewer
	namespaces NamespaceLister
	config     KubernetesConfig
	now        func() time.Time

	// inflight sends one review per decision key at a time; concurrent
	// misses of the same key share its answer.
	inflight singleflight.Group

	mu        sync.Mutex
	decisions map[decisionKey]decision
	sweptAt   time.Time
	roster    []string
	rosterAt  time.Time
}

var _ CollectionAuthorizer = (*KubernetesAuthorizer)(nil)

type decisionKey struct {
	user, groups, verb string
	resource           schema.GroupResource
	namespace, name    string
}

// String names the key unambiguously, for the reviews in flight.
func (k decisionKey) String() string {
	return fmt.Sprintf("%q %q %q %q %q %q %q", k.user, k.groups, k.verb, k.resource.Group, k.resource.Resource, k.namespace, k.name)
}

type decision struct {
	allowed bool
	reason  string
	expires time.Time
}

// NewKubernetesAuthorizer returns an authorizer that reviews with reviews and,
// when config names no namespaces, lists them with namespaces.
func NewKubernetesAuthorizer(reviews SubjectAccessReviewer, namespaces NamespaceLister, config KubernetesConfig) (*KubernetesAuthorizer, error) {
	if reviews == nil {
		return nil, errors.New("a SubjectAccessReview client is required")
	}
	if len(config.Namespaces) == 0 && namespaces == nil {
		return nil, errors.New("a Namespace client is required when no namespaces are named")
	}
	if config.DecisionTTL < 0 {
		return nil, errors.New("the decision TTL must not be negative")
	}
	if config.DecisionTTL == 0 {
		config.DecisionTTL = defaultDecisionTTL
	}
	config.Namespaces = slices.Clone(config.Namespaces)
	config.IncludedNamespaces = slices.Clone(config.IncludedNamespaces)
	return &KubernetesAuthorizer{
		reviews: reviews, namespaces: namespaces, config: config, now: time.Now,
		decisions: map[decisionKey]decision{},
	}, nil
}

// Check reviews one operation. A Kubernetes object is reviewed as itself; a new
// Session is reviewed as a create of the sessions resource in its Agent's
// namespace; an existing control-plane record stays with its owner scope.
func (k *KubernetesAuthorizer) Check(ctx context.Context, principal Principal, verb Verb, resource Resource) error {
	if isControlPlane(principal) {
		return nil
	}
	if resource.Type == allCreatorsResource {
		return errors.New("the Sessions of every creator are not listed")
	}
	target, reviewed, err := reviewedResource(verb, resource)
	if err != nil || !reviewed {
		return err
	}
	subject, err := k.subject(principal)
	if err != nil {
		return err
	}
	allowed, reason, err := k.review(ctx, subject, verb, target, resource.Namespace, resource.Name)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("%s may not %s %s%s", subject.user, verb, describeResource(target, resource.Namespace, resource.Name), reasonSuffix(reason))
	}
	return nil
}

func describeResource(resource schema.GroupResource, namespace, name string) string {
	description := resource.String()
	if name != "" {
		description += " " + name
	}
	if namespace != "" {
		description += " in namespace " + namespace
	}
	return description
}

func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return ": " + reason
}

// Scope answers a collection read with the namespaces in which the principal
// may perform verb on the resource type.
func (k *KubernetesAuthorizer) Scope(ctx context.Context, principal Principal, verb Verb, resourceType string) (authorization.AuthorizationScope, error) {
	if isControlPlane(principal) {
		return authorization.AuthorizationScope{Kind: authorization.ScopeAll}, nil
	}
	if resourceType == allCreatorsResource {
		return authorization.AuthorizationScope{Kind: authorization.ScopeNone}, nil
	}
	target, ok := kubernetesResources[resourceType]
	if !ok {
		if permittedResources[resourceType] {
			return authorization.AuthorizationScope{Kind: authorization.ScopeAll}, nil
		}
		return authorization.AuthorizationScope{}, fmt.Errorf("unknown resource type %q", resourceType)
	}
	subject, err := k.subject(principal)
	if err != nil {
		// A caller whose identity maps to no reviewable subject is allowed
		// nowhere: an empty collection, as for any caller allowed nowhere.
		return authorization.AuthorizationScope{Kind: authorization.ScopeNone}, nil
	}
	// A cluster-wide grant covers every namespace at once.
	allowed, _, err := k.review(ctx, subject, verb, target, "", "")
	if err != nil {
		return authorization.AuthorizationScope{}, err
	}
	if allowed {
		return authorization.AuthorizationScope{Kind: authorization.ScopeAll}, nil
	}
	namespaces, err := k.tenantNamespaces(ctx)
	if err != nil {
		return authorization.AuthorizationScope{}, err
	}
	permitted := make([]bool, len(namespaces))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(scopeReviewConcurrency)
	for i, namespace := range namespaces {
		group.Go(func() error {
			var err error
			permitted[i], _, err = k.review(groupCtx, subject, verb, target, namespace, "")
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return authorization.AuthorizationScope{}, err
	}
	var values []string
	for i, namespace := range namespaces {
		if permitted[i] {
			values = append(values, namespace)
		}
	}
	if len(values) == 0 {
		return authorization.AuthorizationScope{Kind: authorization.ScopeNone}, nil
	}
	return authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{{
		All: []authorization.ScopePredicate{{Attribute: authorization.AttributeNamespace, Operator: authorization.ScopeIn, Values: values}},
	}}}, nil
}

// isControlPlane reports a call of the controller itself, which carries no
// principal at all (ControlPlaneSession). A runtime actor's principal names
// an agent and is not the control plane.
func isControlPlane(principal Principal) bool {
	return principal.User.ID == "" && principal.Agent.ID == "" && principal.Claims == nil
}

// reviewedResource names the Kubernetes resource an operation is reviewed as,
// and whether it is reviewed at all.
func reviewedResource(verb Verb, resource Resource) (schema.GroupResource, bool, error) {
	if target, ok := kubernetesResources[resource.Type]; ok {
		return target, true, nil
	}
	if !permittedResources[resource.Type] {
		return schema.GroupResource{}, false, fmt.Errorf("unknown resource type %q", resource.Type)
	}
	// A Session that is not yet named is a new one, created in its Agent's
	// namespace. A named one exists, and a write on it (a turn, a checkpoint)
	// stays with the owner or share that the service resolves.
	if resource.Type == "Session" && verb == VerbCreate && resource.Name == "" {
		return sessionsResource, true, nil
	}
	return schema.GroupResource{}, false, nil
}

type reviewSubject struct {
	user   string
	groups []string
}

// subject maps the principal to the subject the API server authenticates from
// the same token: the user ID and the groups, each with its prefix. A name in
// the reserved system: space is refused: it would let a caller that names its
// own user ID or groups pick a ServiceAccount's or an administrator's RBAC.
func (k *KubernetesAuthorizer) subject(principal Principal) (reviewSubject, error) {
	if principal.User.ID == "" {
		return reviewSubject{}, errors.New("the caller has no user identity")
	}
	user := k.config.UsernamePrefix + principal.User.ID
	if strings.HasPrefix(user, reservedPrefix) {
		return reviewSubject{}, fmt.Errorf("the user %q is reserved for Kubernetes", user)
	}
	groups := []string{AuthenticatedGroup}
	for _, group := range principal.User.Groups {
		group = k.config.GroupsPrefix + group
		if strings.HasPrefix(group, reservedPrefix) {
			return reviewSubject{}, fmt.Errorf("the group %q is reserved for Kubernetes", group)
		}
		groups = append(groups, group)
	}
	slices.Sort(groups)
	return reviewSubject{user: user, groups: slices.Compact(groups)}, nil
}

// review answers one SubjectAccessReview, from the cache while its decision
// is fresh.
func (k *KubernetesAuthorizer) review(ctx context.Context, subject reviewSubject, verb Verb, resource schema.GroupResource, namespace, name string) (bool, string, error) {
	key := decisionKey{user: subject.user, groups: strings.Join(subject.groups, "\x00"), verb: string(verb), resource: resource, namespace: namespace, name: name}
	now := k.now()
	k.mu.Lock()
	cached, ok := k.decisions[key]
	k.mu.Unlock()
	if ok && now.Before(cached.expires) {
		return cached.allowed, cached.reason, nil
	}
	shared := k.inflight.DoChan(key.String(), func() (any, error) {
		// The review outlives a caller that gives up, so the callers sharing
		// it still get its answer; its own bound keeps a stalled API server
		// from holding them past reviewTimeout.
		reviewCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reviewTimeout)
		defer cancel()
		return k.send(reviewCtx, key, subject, verb, resource, namespace, name)
	})
	select {
	case <-ctx.Done():
		// The caller leaves at its own deadline; the shared review goes on.
		return false, "", fmt.Errorf("%w: %w", ErrReviewUnavailable, ctx.Err())
	case result := <-shared:
		if result.Err != nil {
			return false, "", result.Err
		}
		decided := result.Val.(decision)
		return decided.allowed, decided.reason, nil
	}
}

// send sends one SubjectAccessReview and caches its decision.
func (k *KubernetesAuthorizer) send(ctx context.Context, key decisionKey, subject reviewSubject, verb Verb, resource schema.GroupResource, namespace, name string) (decision, error) {
	review := &authorizationv1.SubjectAccessReview{Spec: authorizationv1.SubjectAccessReviewSpec{
		User: subject.user, Groups: subject.groups,
		ResourceAttributes: &authorizationv1.ResourceAttributes{
			Group: resource.Group, Resource: resource.Resource, Verb: string(verb), Namespace: namespace, Name: name,
		},
	}}
	result, err := k.reviews.Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return decision{}, fmt.Errorf("%w: failed to review %s %s for %s: %w", ErrReviewUnavailable, verb, describeResource(resource, namespace, name), subject.user, err)
	}
	if result.Status.EvaluationError != "" {
		return decision{}, fmt.Errorf("%w: failed to review %s %s for %s: %s", ErrReviewUnavailable, verb, describeResource(resource, namespace, name), subject.user, result.Status.EvaluationError)
	}
	// Allowed without Denied is the only permission; no opinion is a refusal.
	now := k.now()
	decided := decision{allowed: result.Status.Allowed && !result.Status.Denied, reason: result.Status.Reason, expires: now.Add(k.config.DecisionTTL)}
	k.mu.Lock()
	k.decisions[key] = decided
	k.sweep(now)
	k.mu.Unlock()
	return decided, nil
}

// sweep drops the expired decisions, at most once per DecisionTTL so that a
// busy cache is not walked on every miss. The caller holds k.mu.
func (k *KubernetesAuthorizer) sweep(now time.Time) {
	if now.Before(k.sweptAt.Add(k.config.DecisionTTL)) {
		return
	}
	for key, stale := range k.decisions {
		if !now.Before(stale.expires) {
			delete(k.decisions, key)
		}
	}
	k.sweptAt = now
}

// tenantNamespaces are the namespaces a collection read ranges over: the
// configured list, or every selected Namespace and the included ones, listed
// once per DecisionTTL.
func (k *KubernetesAuthorizer) tenantNamespaces(ctx context.Context) ([]string, error) {
	if len(k.config.Namespaces) > 0 {
		return k.config.Namespaces, nil
	}
	now := k.now()
	k.mu.Lock()
	roster, fresh := k.roster, !k.rosterAt.IsZero() && now.Before(k.rosterAt.Add(k.config.DecisionTTL))
	k.mu.Unlock()
	if fresh {
		return roster, nil
	}
	options := metav1.ListOptions{}
	if k.config.NamespaceSelector != nil {
		options.LabelSelector = k.config.NamespaceSelector.String()
	}
	list, err := k.namespaces.List(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to list namespaces: %w", ErrReviewUnavailable, err)
	}
	roster = make([]string, 0, len(list.Items)+len(k.config.IncludedNamespaces))
	for _, namespace := range list.Items {
		roster = append(roster, namespace.Name)
	}
	roster = append(roster, k.config.IncludedNamespaces...)
	slices.Sort(roster)
	roster = slices.Compact(roster)
	k.mu.Lock()
	k.roster, k.rosterAt = roster, now
	k.mu.Unlock()
	return roster, nil
}
