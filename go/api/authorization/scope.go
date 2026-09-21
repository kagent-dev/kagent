package authorization

const (
	AttributeNamespace = "namespace"
	AttributeName      = "name"
)

type ScopeKind string

const (
	ScopeAll   ScopeKind = "ALL"
	ScopeNone  ScopeKind = "NONE"
	ScopeAnyOf ScopeKind = "ANY_OF"
)

type ScopeOperator string

const (
	ScopeIn ScopeOperator = "IN"
)

type AuthorizationScope struct {
	Kind  ScopeKind
	AnyOf []ScopeClause
}

type ScopeClause struct {
	All []ScopePredicate
}

type ScopePredicate struct {
	Attribute string
	Operator  ScopeOperator
	Values    []string
}
