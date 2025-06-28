# Role
You are an Argo Rollout Generator that creates valid YAML configurations based on user request.
The request might mention multiple resources and tasks, but you only focus on the Argo Rollout.

If no rollout name is provided, default to 'rollout'. If a deployment is mentioned, use its name as the rollout name.

If a deployment is referenced, map its template, selector, and strategy fields directly to the corresponding rollout fields,
as both resources use corev1.PodTemplateSpec and define similar behaviors.

# Context

apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.13.0
    helm.sh/resource-policy: keep
    meta.helm.sh/release-name: argo-rollouts
    meta.helm.sh/release-namespace: argo-rollouts
  creationTimestamp: "2025-02-17T18:46:50Z"
  generation: 1
  labels:
    app.kubernetes.io/managed-by: Helm
    app.kubernetes.io/name: argo-rollouts
    app.kubernetes.io/part-of: argo-rollouts
  name: rollouts.argoproj.io
  resourceVersion: "94172"
  uid: 4d23313f-12c9-48cc-bd80-7c2521f138c3
spec:
  conversion:
    strategy: None
  group: argoproj.io
  names:
    kind: Rollout
    listKind: RolloutList
    plural: rollouts
    shortNames:
    - ro
    singular: rollout
  scope: Namespaced
  versions:
  - additionalPrinterColumns:
    - description: Number of desired pods
      jsonPath: .spec.replicas
      name: Desired
      type: integer
    - description: Total number of non-terminated pods targeted by this rollout
      jsonPath: .status.replicas
      name: Current
      type: integer
    - description: Total number of non-terminated pods targeted by this rollout that
        have the desired template spec
      jsonPath: .status.updatedReplicas
      name: Up-to-date
      type: integer
    - description: Total number of available pods (ready for at least minReadySeconds)
        targeted by this rollout
      jsonPath: .status.availableReplicas
      name: Available
      type: integer
    - description: Time since resource was created
      jsonPath: .metadata.creationTimestamp
      name: Age
      type: date
    name: v1alpha1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            type: string
          kind:
            type: string
          metadata:
            type: object
          spec:
            properties:
              analysis:
                properties:
                  successfulRunHistoryLimit:
                    format: int32
                    type: integer
                  unsuccessfulRunHistoryLimit:
                    format: int32
                    type: integer
                type: object
              minReadySeconds:
                format: int32
                type: integer
              paused:
                type: boolean
              progressDeadlineAbort:
                type: boolean
              progressDeadlineSeconds:
                format: int32
                type: integer
              replicas:
                format: int32
                type: integer
              restartAt:
                format: date-time
                type: string
              revisionHistoryLimit:
                format: int32
                type: integer
              rollbackWindow:
                properties:
                  revisions:
                    format: int32
                    type: integer
                type: object
              selector:
                properties:
                  matchExpressions:
                    items:
                      properties:
                        key:
                          type: string
                        operator:
                          type: string
                        values:
                          items:
                            type: string
                          type: array
                      required:
                      - key
                      - operator
                      type: object
                    type: array
                  matchLabels:
                    additionalProperties:
                      type: string
                    type: object
                type: object
                x-kubernetes-map-type: atomic
              strategy:
                properties:
                  blueGreen:
                    properties:
                      abortScaleDownDelaySeconds:
                        format: int32
                        type: integer
                      activeMetadata:
                        properties:
                          annotations:
                            additionalProperties:
                              type: string
                            type: object
                          labels:
                            additionalProperties:
                              type: string
                            type: object
                        type: object
                      activeService:
                        type: string
                      antiAffinity:
                        properties:
                          preferredDuringSchedulingIgnoredDuringExecution:
                            properties:
                              weight:
                                format: int32
                                type: integer
                            required:
                            - weight
                            type: object
                          requiredDuringSchedulingIgnoredDuringExecution:
                            type: object
                        type: object
                      autoPromotionEnabled:
                        type: boolean
                      autoPromotionSeconds:
                        format: int32
                        type: integer
                      maxUnavailable:
                        anyOf:
                        - type: integer
                        - type: string
                        x-kubernetes-int-or-string: true
                      postPromotionAnalysis:
                        properties:
                          analysisRunMetadata:
                            properties:
                              annotations:
                                additionalProperties:
                                  type: string
                                type: object
                              labels:
                                additionalProperties:
                                  type: string
                                type: object
                            type: object
                          args:
                            items:
                              properties:
                                name:
                                  type: string
                                value:
                                  type: string
                                valueFrom:
                                  properties:
                                    fieldRef:
                                      properties:
                                        fieldPath:
                                          type: string
                                      required:
                                      - fieldPath
                                      type: object
                                    podTemplateHashValue:
                                      type: string
                                  type: object
                              required:
                              - name
                              type: object
                            type: array
                          dryRun:
                            items:
                              properties:
                                metricName:
                                  type: string
                              required:
                              - metricName
                              type: object
                            type: array
                          measurementRetention:
                            items:
                              properties:
                                limit:
                                  format: int32
                                  type: integer
                                metricName:
                                  type: string
                              required:
                              - limit
                              - metricName
                              type: object
                            type: array
                          templates:
                            items:
                              properties:
                                clusterScope:
                                  type: boolean
                                templateName:
                                  type: string
                              type: object
                            type: array
                        type: object
                      prePromotionAnalysis:
                        properties:
                          analysisRunMetadata:
                            properties:
                              annotations:
                                additionalProperties:
                                  type: string
                                type: object
                              labels:
                                additionalProperties:
                                  type: string
                                type: object
                            type: object
                          args:
                            items:
                              properties:
                                name:
                                  type: string
                                value:
                                  type: string
                                valueFrom:
                                  properties:
                                    fieldRef:
                                      properties:
                                        fieldPath:
                                          type: string
                                      required:
                                      - fieldPath
                                      type: object
                                    podTemplateHashValue:
                                      type: string
                                  type: object
                              required:
                              - name
                              type: object
                            type: array
                          dryRun:
                            items:
                              properties:
                                metricName:
                                  type: string
                              required:
                              - metricName
                              type: object
                            type: array
                          measurementRetention:
                            items:
                              properties:
                                limit:
                                  format: int32
                                  type: integer
                                metricName:
                                  type: string
                              required:
                              - limit
                              - metricName
                              type: object
                            type: array
                          templates:
                            items:
                              properties:
                                clusterScope:
                                  type: boolean
                                templateName:
                                  type: string
                              type: object
                            type: array
                        type: object
                      previewMetadata:
                        properties:
                          annotations:
                            additionalProperties:
                              type: string
                            type: object
                          labels:
                            additionalProperties:
                              type: string
                            type: object
                        type: object
                      previewReplicaCount:
                        format: int32
                        type: integer
                      previewService:
                        type: string
                      scaleDownDelayRevisionLimit:
                        format: int32
                        type: integer
                      scaleDownDelaySeconds:
                        format: int32
                        type: integer
                    required:
                    - activeService
                    type: object
                  canary:
                    properties:
                      abortScaleDownDelaySeconds:
                        format: int32
                        type: integer
                      analysis:
                        properties:
                          analysisRunMetadata:
                            properties:
                              annotations:
                                additionalProperties:
                                  type: string
                                type: object
                              labels:
                                additionalProperties:
                                  type: string
                                type: object
                            type: object
                          args:
                            items:
                              properties:
                                name:
                                  type: string
                                value:
                                  type: string
                                valueFrom:
                                  properties:
                                    fieldRef:
                                      properties:
                                        fieldPath:
                                          type: string
                                      required:
                                      - fieldPath
                                      type: object
                                    podTemplateHashValue:
                                      type: string
                                  type: object
                              required:
                              - name
                              type: object
                            type: array
                          dryRun:
                            items:
                              properties:
                                metricName:
                                  type: string
                              required:
                              - metricName
                              type: object
                            type: array
                          measurementRetention:
                            items:
                              properties:
                                limit:
                                  format: int32
                                  type: integer
                                metricName:
                                  type: string
                              required:
                              - limit
                              - metricName
                              type: object
                            type: array
                          startingStep:
                            format: int32
                            type: integer
                          templates:
                            items:
                              properties:
                                clusterScope:
                                  type: boolean
                                templateName:
                                  type: string
                              type: object
                            type: array
                        type: object
                      antiAffinity:
                        properties:
                          preferredDuringSchedulingIgnoredDuringExecution:
                            properties:
                              weight:
                                format: int32
                                type: integer
                            required:
                            - weight
                            type: object
                          requiredDuringSchedulingIgnoredDuringExecution:
                            type: object
                        type: object
                      canaryMetadata:
                        properties:
                          annotations:
                            additionalProperties:
                              type: string
                            type: object
                          labels:
                            additionalProperties:
                              type: string
                            type: object
                        type: object
                      canaryService:
                        type: string
                      dynamicStableScale:
                        type: boolean
                      maxSurge:
                        anyOf:
                        - type: integer
                        - type: string
                        x-kubernetes-int-or-string: true
                      maxUnavailable:
                        anyOf:
                        - type: integer
                        - type: string
                        x-kubernetes-int-or-string: true
                      minPodsPerReplicaSet:
                        format: int32
                        type: integer
                      pingPong:
                        properties:
                          pingService:
                            type: string
                          pongService:
                            type: string
                        required:
                        - pingService
                        - pongService
                        type: object
                      scaleDownDelayRevisionLimit:
                        format: int32
                        type: integer
                      scaleDownDelaySeconds:
                        format: int32
                        type: integer
                      stableMetadata:
                        properties:
                          annotations:
                            additionalProperties:
                              type: string
                            type: object
                          labels:
                            additionalProperties:
                              type: string
                            type: object
                        type: object
                      stableService:
                        type: string
                      steps:
                        items:
                          properties:
                            analysis:
                              properties:
                                analysisRunMetadata:
                                  properties:
                                    annotations:
                                      additionalProperties:
                                        type: string
                                      type: object
                                    labels:
                                      additionalProperties:
                                        type: string
                                      type: object
                                  type: object
                                args:
                                  items:
                                    properties:
                                      name:
                                        type: string
                                      value:
                                        type: string
                                      valueFrom:
                                        properties:
                                          fieldRef:
                                            properties:
                                              fieldPath:
                                                type: string
                                            required:
                                            - fieldPath
                                            type: object
                                          podTemplateHashValue:
                                            type: string
                                        type: object
                                    required:
                                    - name
                                    type: object
                                  type: array
                                dryRun:
                                  items:
                                    properties:
                                      metricName:
                                        type: string
                                    required:
                                    - metricName
                                    type: object
                                  type: array
                                measurementRetention:
                                  items:
                                    properties:
                                      limit:
                                        format: int32
                                        type: integer
                                      metricName:
                                        type: string
                                    required:
                                    - limit
                                    - metricName
                                    type: object
                                  type: array
                                templates:
                                  items:
                                    properties:
                                      clusterScope:
                                        type: boolean
                                      templateName:
                                        type: string
                                    type: object
                                  type: array
                              type: object
                            experiment:
                              properties:
                                analyses:
                                  items:
                                    properties:
                                      args:
                                        items:
                                          properties:
                                            name:
                                              type: string
                                            value:
                                              type: string
                                            valueFrom:
                                              properties:
                                                fieldRef:
                                                  properties:
                                                    fieldPath:
                                                      type: string
                                                  required:
                                                  - fieldPath
                                                  type: object
                                                podTemplateHashValue:
                                                  type: string
                                              type: object
                                          required:
                                          - name
                                          type: object
                                        type: array
                                      clusterScope:
                                        type: boolean
                                      name:
                                        type: string
                                      requiredForCompletion:
                                        type: boolean
                                      templateName:
                                        type: string
                                    required:
                                    - name
                                    - templateName
                                    type: object
                                  type: array
                                analysisRunMetadata:
                                  properties:
                                    annotations:
                                      additionalProperties:
                                        type: string
                                      type: object
                                    labels:
                                      additionalProperties:
                                        type: string
                                      type: object
                                  type: object
                                dryRun:
                                  items:
                                    properties:
                                      metricName:
                                        type: string
                                    required:
                                    - metricName
                                    type: object
                                  type: array
                                duration:
                                  type: string
                                templates:
                                  items:
                                    properties:
                                      metadata:
                                        properties:
                                          annotations:
                                            additionalProperties:
                                              type: string
                                            type: object
                                          labels:
                                            additionalProperties:
                                              type: string
                                            type: object
                                        type: object
                                      name:
                                        type: string
                                      replicas:
                                        format: int32
                                        type: integer
                                      selector:
                                        properties:
                                          matchExpressions:
                                            items:
                                              properties:
                                                key:
                                                  type: string
                                                operator:
                                                  type: string
                                                values:
                                                  items:
                                                    type: string
                                                  type: array
                                              required:
                                              - key
                                              - operator
                                              type: object
                                            type: array
                                          matchLabels:
                                            additionalProperties:
                                              type: string
                                            type: object
                                        type: object
                                        x-kubernetes-map-type: atomic
                                      service:
                                        properties:
                                          name:
                                            type: string
                                        type: object
                                      specRef:
                                        type: string
                                      weight:
                                        format: int32
                                        type: integer
                                    required:
                                    - name
                                    - specRef
                                    type: object
                                  type: array
                              required:
                              - templates
                              type: object
                            pause:
                              properties:
                                duration:
                                  anyOf:
                                  - type: integer
                                  - type: string
                                  x-kubernetes-int-or-string: true
                              type: object
                            setCanaryScale:
                              properties:
                                matchTrafficWeight:
                                  type: boolean
                                replicas:
                                  format: int32
                                  type: integer
                                weight:
                                  format: int32
                                  type: integer
                              type: object
                            setHeaderRoute:
                              properties:
                                match:
                                  items:
                                    properties:
                                      headerName:
                                        type: string
                                      headerValue:
                                        properties:
                                          exact:
                                            type: string
                                          prefix:
                                            type: string
                                          regex:
                                            type: string
                                        type: object
                                    required:
                                    - headerName
                                    - headerValue
                                    type: object
                                  type: array
                                name:
                                  type: string
                              type: object
                            setMirrorRoute:
                              properties:
                                match:
                                  items:
                                    properties:
                                      headers:
                                        additionalProperties:
                                          properties:
                                            exact:
                                              type: string
                                            prefix:
                                              type: string
                                            regex:
                                              type: string
                                          type: object
                                        type: object
                                      method:
                                        properties:
                                          exact:
                                            type: string
                                          prefix:
                                            type: string
                                          regex:
                                            type: string
                                        type: object
                                      path:
                                        properties:
                                          exact:
                                            type: string
                                          prefix:
                                            type: string
                                          regex:
                                            type: string
                                        type: object
                                    type: object
                                  type: array
                                name:
                                  type: string
                                percentage:
                                  format: int32
                                  type: integer
                              required:
                              - name
                              type: object
                            setWeight:
                              format: int32
                              type: integer
                          type: object
                        type: array
                      trafficRouting:
                        properties:
                          alb:
                            properties:
                              annotationPrefix:
                                type: string
                              ingress:
                                type: string
                              ingresses:
                                items:
                                  type: string
                                type: array
                              rootService:
                                type: string
                              servicePort:
                                format: int32
                                type: integer
                              stickinessConfig:
                                properties:
                                  durationSeconds:
                                    format: int64
                                    type: integer
                                  enabled:
                                    type: boolean
                                required:
                                - durationSeconds
                                - enabled
                                type: object
                            required:
                            - servicePort
                            type: object
                          ambassador:
                            properties:
                              mappings:
                                items:
                                  type: string
                                type: array
                            required:
                            - mappings
                            type: object
                          apisix:
                            properties:
                              route:
                                properties:
                                  name:
                                    type: string
                                  rules:
                                    items:
                                      type: string
                                    type: array
                                required:
                                - name
                                type: object
                            type: object
                          appMesh:
                            properties:
                              virtualNodeGroup:
                                properties:
                                  canaryVirtualNodeRef:
                                    properties:
                                      name:
                                        type: string
                                    required:
                                    - name
                                    type: object
                                  stableVirtualNodeRef:
                                    properties:
                                      name:
                                        type: string
                                    required:
                                    - name
                                    type: object
                                required:
                                - canaryVirtualNodeRef
                                - stableVirtualNodeRef
                                type: object
                              virtualService:
                                properties:
                                  name:
                                    type: string
                                  routes:
                                    items:
                                      type: string
                                    type: array
                                required:
                                - name
                                type: object
                            type: object
                          istio:
                            properties:
                              destinationRule:
                                properties:
                                  canarySubsetName:
                                    type: string
                                  name:
                                    type: string
                                  stableSubsetName:
                                    type: string
                                required:
                                - canarySubsetName
                                - name
                                - stableSubsetName
                                type: object
                              virtualService:
                                properties:
                                  name:
                                    type: string
                                  routes:
                                    items:
                                      type: string
                                    type: array
                                  tcpRoutes:
                                    items:
                                      properties:
                                        port:
                                          format: int64
                                          type: integer
                                      type: object
                                    type: array
                                  tlsRoutes:
                                    items:
                                      properties:
                                        port:
                                          format: int64
                                          type: integer
                                        sniHosts:
                                          items:
                                            type: string
                                          type: array
                                      type: object
                                    type: array
                                required:
                                - name
                                type: object
                              virtualServices:
                                items:
                                  properties:
                                    name:
                                      type: string
                                    routes:
                                      items:
                                        type: string
                                      type: array
                                    tcpRoutes:
                                      items:
                                        properties:
                                          port:
                                            format: int64
                                            type: integer
                                        type: object
                                      type: array
                                    tlsRoutes:
                                      items:
                                        properties:
                                          port:
                                            format: int64
                                            type: integer
                                          sniHosts:
                                            items:
                                              type: string
                                            type: array
                                        type: object
                                      type: array
                                  required:
                                  - name
                                  type: object
                                type: array
                            type: object
                          managedRoutes:
                            items:
                              properties:
                                name:
                                  type: string
                              required:
                              - name
                              type: object
                            type: array
                          maxTrafficWeight:
                            format: int32
                            type: integer
                          nginx:
                            properties:
                              additionalIngressAnnotations:
                                additionalProperties:
                                  type: string
                                type: object
                              annotationPrefix:
                                type: string
                              stableIngress:
                                type: string
                              stableIngresses:
                                items:
                                  type: string
                                type: array
                            type: object
                          plugins:
                            type: object
                            x-kubernetes-preserve-unknown-fields: true
                          smi:
                            properties:
                              rootService:
                                type: string
                              trafficSplitName:
                                type: string
                            type: object
                          traefik:
                            properties:
                              weightedTraefikServiceName:
                                type: string
                            required:
                            - weightedTraefikServiceName
                            type: object
                        type: object
                    type: object
                type: object
              template:
                properties:
                  metadata:
                    properties:
                      annotations:
                        additionalProperties:
                          type: string
                        type: object
                      labels:
                        additionalProperties:
                          type: string
                        type: object
                    type: object
                  spec:
                    properties:
                      activeDeadlineSeconds:
                        format: int64
                        type: integer
                      affinity:
                        properties:
                          nodeAffinity:
                            properties:
                              preferredDuringSchedulingIgnoredDuringExecution:
                                items:
                                  properties:
                                    preference:
                                      properties:
                                        matchExpressions:
                                          items:
                                            properties:
                                              key:
                                                type: string
                                              operator:
                                                type: string
                                              values:
                                                items:
                                                  type: string
                                                type: array
                                            required:
                                            - key
                                            - operator
                                            type: object
                                          type: array
                                        matchFields:
                                          items:
                                            properties:
                                              key:
                                                type: string
                                              operator:
                                                type: string
                                              values:
                                                items:
                                                  type: string
                                                type: array
                                            required:
                                            - key
                                            - operator
                                            type: object
                                          type: array
                                      type: object
                                      x-kubernetes-map-type: atomic
                                    weight:
                                      format: int32
                                      type: integer
                                  required:
                                  - preference
                                  - weight
                                  type: object
                                type: array
                              requiredDuringSchedulingIgnoredDuringExecution:
                                properties:
                                  nodeSelectorTerms:
                                    items:
                                      properties:
                                        matchExpressions:
                                          items:
                                            properties:
                                              key:
                                                type: string
                                              operator:
                                                type: string
                                              values:
                                                items:
                                                  type: string
                                                type: array
                                            required:
                                            - key
                                            - operator
                                            type: object
                                          type: array
                                        matchFields:
                                          items:
                                            properties:
                                              key:
                                                type: string
                                              operator:
                                                type: string
                                              values:
                                                items:
                                                  type: string
                                                type: array
                                            required:
                                            - key
                                            - operator
                                            type: object
                                          type: array
                                      type: object
                                      x-kubernetes-map-type: atomic
                                    type: array
                                required:
                                - nodeSelectorTerms
                                type: object
                                x-kubernetes-map-type: atomic
                            type: object
                          podAffinity:
                            properties:
                              preferredDuringSchedulingIgnoredDuringExecution:
                                items:
                                  properties:
                                    podAffinityTerm:
                                      properties:
                                        labelSelector:
                                          properties:
                                            matchExpressions:
                                              items:
                                                properties:
                                                  key:
                                                    type: string
                                                  operator:
                                                    type: string
                                                  values:
                                                    items:
                                                      type: string
                                                    type: array
                                                required:
                                                - key
                                                - operator
                                                type: object
                                              type: array
                                            matchLabels:
                                              additionalProperties:
                                                type: string
                                              type: object
                                          type: object
                                          x-kubernetes-map-type: atomic
                                        matchLabelKeys:
                                          items:
                                            type: string
                                          type: array
                                          x-kubernetes-list-type: atomic
                                        mismatchLabelKeys:
                                          items:
                                            type: string
                                          type: array
                                          x-kubernetes-list-type: atomic
                                        namespaceSelector:
                                          properties:
                                            matchExpressions:
                                              items:
                                                properties:
                                                  key:
                                                    type: string
                                                  operator:
                                                    type: string
                                                  values:
                                                    items:
                                                      type: string
                                                    type: array
                                                required:
                                                - key
                                                - operator
                                                type: object
                                              type: array
                                            matchLabels:
                                              additionalProperties:
                                                type: string
                                              type: object
                                          type: object
                                          x-kubernetes-map-type: atomic
                                        namespaces:
                                          items:
                                            type: string
                                          type: array
                                        topologyKey:
                                          type: string
                                      required:
                                      - topologyKey
                                      type: object
                                    weight:
                                      format: int32
                                      type: integer
                                  required:
                                  - podAffinityTerm
                                  - weight
                                  type: object
                                type: array
                              requiredDuringSchedulingIgnoredDuringExecution:
                                items:
                                  properties:
                                    labelSelector:
                                      properties:
                                        matchExpressions:
                                          items:
                                            properties:
                                              key:
                                                type: string
                                              operator:
                                                type: string
                                              values:
                                                items:
                                                  type: string
                                                type: array
                                            required:
                                            - key
                                            - operator
                                            type: object
                                          type: array
                                        matchLabels:
                                          additionalProperties:
                                            type: string
                                          type: object
                                      type: object
                                      x-kubernetes-map-type: atomic
                                    matchLabelKeys:
                                      items:
                                        type: string
                                      type: array
                                      x-kubernetes-list-type: atomic
                                    mismatchLabelKeys:
                                      items:
                                        type: string
                                      type: array
                                      x-kubernetes-list-type: atomic
                                    namespaceSelector:
                                      properties:
                                        matchExpressions:
                                          items:
                                            properties:
                                              key:
                                                type: string
                                              operator:
                                                type: string
                                              values:
                                                items:
                                                  type: string
                                                type: array
                                                required:
                                                - key
                                                - operator
                                                type: object
                                              type: array
                                            matchLabels:
                                              additionalProperties:
                                                type: string
                                              type: object
                                          type: object
                                          x-kubernetes-map-type: atomic
                                        namespaces:
                                          items:
                                            type: string
                                          type: array
                                        topologyKey:
                                          type: string
                                      required:
                                      - topologyKey
                                      type: object
                                    weight:
                                      format: int32
                                      type: integer
                                  required:
                                  - topologyKey
                                  type: object
                                type: array
                            type: object
                          podAntiAffinity:
                            properties:
                              preferredDuringSchedulingIgnoredDuringExecution:
                                items:
                                  properties:
                                    podAffinityTerm:
                                      properties:
                                        labelSelector:
                                          properties:
                                            matchExpressions:
                                              items:
                                                properties:
                                                  key:
                                                    type: string
                                                  operator:
                                                    type: string
                                                  values:
                                                    items:
                                                      type: string
                                                    type: array
                                                required:
                                                - key
                                                - operator
                                                type: object
                                              type: array
                                            matchLabels:
                                              additionalProperties:
                                                type: string
                                              type: object
                                          type: object
                                          x-kubernetes-map-type: atomic
                                        matchLabelKeys:
                                          items:
                                            type: string
                                          type: array
                                          x-kubernetes-list-type: atomic
                                        mismatchLabelKeys:
                                          items:
                                            type: string
                                          type: array
                                          x-kubernetes-list-type: atomic
                                        namespaceSelector:
                                          properties:
                                            matchExpressions:
                                              items:
                                                properties:
                                                  key:
                                                    type: string
                                                  operator:
                                                    type: string
                                                  values:
                                                    items:
                                                      type: string
                                                    type: array
                                                required:
                                                - key
                                                - operator
                                                type: object
                                              type: array
                                            matchLabels:
                                              additionalProperties:
                                                type: string
                                              type: object
                                          type: object
                                          x-kubernetes-map-type: atomic
                                        namespaces:
                                          items:
                                            type: string
                                          type: array
                                        topologyKey:
                                          type: string
                                      required:
                                      - topologyKey
                                      type: object
                                    weight:
                                      format: int32
                                      type: integer
                                  required:
                                  - podAffinityTerm
                                  - weight
                                  type: object
                                type: array
                              requiredDuringSchedulingIgnoredDuringExecution:
                                items:
                                  properties:
                                    labelSelector:
                                      properties:
                                        matchExpressions:
                                          items:
                                            properties:
                                              key:
                                                type: string
                                              operator:
                                                type: string
                                              values:
                                                items:
                                                  type: string
                                                type: array
                                            required:
                                            - key
                                            - operator
                                            type: object
                                          type: array
                                        matchLabels:
                                          additionalProperties:
                                            type: string
                                          type: object
                                      type: object
                                      x-kubernetes-map-type: atomic
                                    matchLabelKeys:
                                      items:
                                        type: string
                                      type: array
                                      x-kubernetes-list-type: atomic
                                    mismatchLabelKeys:
                                      items:
                                        type: string
                                      type: array
                                      x-kubernetes-list-type: atomic
                                    namespaceSelector:
                                      properties:
                                        matchExpressions:
                                          items:
                                            properties:
                                              key:
                                                type: string
                                              operator:
                                                type: string
                                              values:
                                                items:
                                                  type: string
                                                type: array
                                                required:
                                                - key
                                                - operator
                                                type: object
                                              type: array
                                            matchLabels:
                                              additionalProperties:
                                                type: string
                                              type: object
                                          type: object
                                          x-kubernetes-map-type: atomic
                                        namespaces:
                                          items:
                                            type: string
                                          type: array
                                        topologyKey:
                                          type: string
                                      required:
                                      - topologyKey
                                      type: object
                                    weight:
                                      format: int32
                                      type: integer
                                  required:
                                  - topologyKey
                                  type: object
                                type: array
                            type: object
                        type: object
                      automountServiceAccountToken:
                        type: boolean
                      containers:
                        items:
                          properties:
                            args:
                              items:
                                type: string
                              type: array
                            command:
                              items:
                                type: string
                              type: array
                            env:
                              items:
                                properties:
                                  name:
                                    type: string
                                  value:
                                    type: string
                                  valueFrom:
                                    properties:
                                      configMapKeyRef:
                                        properties:
                                          key:
                                            type: string
                                          name:
                                            type: string
                                          optional:
                                            type: boolean
                                        required:
                                        - key
                                        type: object
                                        x-kubernetes-map-type: atomic
                                      fieldRef:
                                        properties:
                                          apiVersion:
                                            type: string
                                          fieldPath:
                                            type: string
                                        required:
                                        - fieldPath
                                        type: object
                                        x-kubernetes-map-type: atomic
                                      resourceFieldRef:
                                        properties:
                                          containerName:
                                            type: string
                                          divisor:
                                            anyOf:
                                            - type: integer
                                            - type: string
                                            pattern: ^(\+|-)?(([0-9[]+(\.[0-9[]*)?)|(\.[0-9[]+))(([KMGTPE[]i)|[numkMGTPE[]|([eE[](\+|-)?(([0-9[]+(\.[0-9[]*)?)|(\.[0-9[]+))))?$
                                            x-kubernetes-int-or-string: true
                                          resource:
                                            type: string
                                        required:
                                        - resource
                                        type: object
                                        x-kubernetes-map-type: atomic
                                      secretKeyRef:
                                        properties:
                                          key:
                                            type: string
                                          name:
                                            type: string
                                          optional:
                                            type: boolean
                                        required:
                                        - key
                                        type: object
                                        x-kubernetes-map-type: atomic
                                    type: object
                                required:
                                - name
                                type: object
                              type: array
                            envFrom:
                              items:
                                properties:
                                  configMapRef:
                                    properties:
                                      name:
                                        type: string
                                      optional:
                                        type: boolean
                                    type: object
                                    x-kubernetes-map-type: atomic
                                  prefix:
                                    type: string
                                  secretRef:
                                    properties:
                                      name:
                                        type: string
                                      optional:
                                        type: boolean
                                    type: object
                                    x-kubernetes-map-type: atomic
                                type: object
                              type: array
                            image:
                              type: string
                            imagePullPolicy:
                              type: string
                            lifecycle:
                              properties:
                                postStart:
                                  properties:
                                    exec:
                                      properties:
                                        command:
                                          items:
                                            type: string
                                          type: array
                                      type: object
                                    httpGet:
                                      properties:
                                        host:
                                          type: string
                                        httpHeaders:
                                          items:
                                            properties:
                                              name:
                                                type: string
                                              value:
                                                type: string
                                            required:
                                            - name
                                            - value
                                            type: object
                                          type: array
                                        path:
                                          type: string
                                        port:
                                          anyOf:
                                          - type: integer
                                          - type: string
                                          x-kubernetes-int-or-string: true
                                        scheme:
                                          type: string
                                      required:
                                      - port
                                      type: object
                                    sleep:
                                      properties:
                                        seconds:
                                          format: int64
                                          type: integer
                                      required:
                                      - seconds
                                      type: object
                                    tcpSocket:
                                      properties:
                                        host:
                                          type: string
                                        port:
                                          anyOf:
                                          - type: integer
                                          - type: string
                                          x-kubernetes-int-or-string: true
                                      required:
                                      - port
                                      type: object
                                  type: object
                                preStop:
                                  properties:
                                    exec:
                                      properties:
                                        command:
                                          items:
                                            type: string
                                          type: array
                                      type: object
                                    httpGet:
                                      properties:
                                        host:
                                          type: string
                                        httpHeaders:
                                          items:
                                            properties:
                                              name:
                                                type: string
                                              value:
                                                type: string
                                            required:
                                            - name
                                            - value
                                            type: object
                                          type: array
                                        path:
                                          type: string
                                        port:
                                          anyOf:
                                          - type: integer
                                          - type: string
                                          x-kubernetes-int-or-string: true
                                        scheme:
                                          type: string
                                      required:
                                      - port
                                      type: object
                                    sleep:
                                      properties:
                                        seconds:
                                          format: int64
                                          type: integer
                                      required:
                                      - seconds
                                      type: object
                                    tcpSocket:
                                      properties:
                                        host:
                                          type: string
                                        port:
                                          anyOf:
                                          - type: integer
                                          - type: string
                                          x-kubernetes-int-or-string: true
                                      required:
                                      - port
                                      type: object
                                  type: object
                              type: object
                            livenessProbe:
                              properties:
                                exec:
                                  properties:
                                    command:
                                      items:
                                        type: string
                                      type: array
                                  type: object
                                failureThreshold:
                                  format: int32
                                  type: integer
                                grpc:
                                  properties:
                                    port:
                                      format: int32
                                      type: integer
                                    service:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                httpGet:
                                  properties:
                                    host:
                                      type: string
                                    httpHeaders:
                                      items:
                                        properties:
                                          name:
                                            type: string
                                          value:
                                            type: string
                                        required:
                                        - name
                                        - value
                                        type: object
                                      type: array
                                    path:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                    scheme:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                initialDelaySeconds:
                                  format: int32
                                  type: integer
                                periodSeconds:
                                  format: int32
                                  type: integer
                                successThreshold:
                                  format: int32
                                  type: integer
                                tcpSocket:
                                  properties:
                                    host:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                  required:
                                  - port
                                  type: object
                                terminationGracePeriodSeconds:
                                  format: int64
                                  type: integer
                                timeoutSeconds:
                                  format: int32
                                  type: integer
                              type: object
                            name:
                              type: string
                            ports:
                              items:
                                properties:
                                  containerPort:
                                    format: int32
                                    type: integer
                                  hostIP:
                                    type: string
                                  hostPort:
                                    format: int32
                                    type: integer
                                  name:
                                    type: string
                                  protocol:
                                    default: TCP
                                    type: string
                                required:
                                - containerPort
                                type: object
                              type: array
                              x-kubernetes-list-map-keys:
                              - containerPort
                              - protocol
                              x-kubernetes-list-type: map
                            readinessProbe:
                              properties:
                                exec:
                                  properties:
                                    command:
                                      items:
                                        type: string
                                      type: array
                                  type: object
                                failureThreshold:
                                  format: int32
                                  type: integer
                                grpc:
                                  properties:
                                    port:
                                      format: int32
                                      type: integer
                                    service:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                httpGet:
                                  properties:
                                    host:
                                      type: string
                                    httpHeaders:
                                      items:
                                        properties:
                                          name:
                                            type: string
                                          value:
                                            type: string
                                        required:
                                        - name
                                        - value
                                        type: object
                                      type: array
                                    path:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                    scheme:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                initialDelaySeconds:
                                  format: int32
                                  type: integer
                                periodSeconds:
                                  format: int32
                                  type: integer
                                successThreshold:
                                  format: int32
                                  type: integer
                                tcpSocket:
                                  properties:
                                    host:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                  required:
                                  - port
                                  type: object
                                terminationGracePeriodSeconds:
                                  format: int64
                                  type: integer
                                timeoutSeconds:
                                  format: int32
                                  type: integer
                              type: object
                            resizePolicy:
                              items:
                                properties:
                                  resourceName:
                                    type: string
                                  restartPolicy:
                                    type: string
                                required:
                                - resourceName
                                - restartPolicy
                                type: object
                              type: array
                              x-kubernetes-list-type: atomic
                            resources:
                              properties:
                                claims:
                                  items:
                                    properties:
                                      name:
                                        type: string
                                    required:
                                    - name
                                    type: object
                                  type: array
                                  x-kubernetes-list-map-keys:
                                  - name
                                  x-kubernetes-list-type: map
                                limits:
                                  x-kubernetes-preserve-unknown-fields: true
                                requests:
                                  x-kubernetes-preserve-unknown-fields: true
                              type: object
                            restartPolicy:
                              type: string
                            securityContext:
                              properties:
                                allowPrivilegeEscalation:
                                  type: boolean
                                capabilities:
                                  properties:
                                    add:
                                      items:
                                        type: string
                                      type: array
                                    drop:
                                      items:
                                        type: string
                                      type: array
                                  type: object
                                privileged:
                                  type: boolean
                                procMount:
                                  type: string
                                readOnlyRootFilesystem:
                                  type: boolean
                                runAsGroup:
                                  format: int64
                                  type: integer
                                runAsNonRoot:
                                  type: boolean
                                runAsUser:
                                  format: int64
                                  type: integer
                                seLinuxOptions:
                                  properties:
                                    level:
                                      type: string
                                    role:
                                      type: string
                                    type:
                                      type: string
                                    user:
                                      type: string
                                  type: object
                                seccompProfile:
                                  properties:
                                    localhostProfile:
                                      type: string
                                    type:
                                      type: string
                                  required:
                                  - type
                                  type: object
                                windowsOptions:
                                  properties:
                                    gmsaCredentialSpec:
                                      type: string
                                    gmsaCredentialSpecName:
                                      type: string
                                    hostProcess:
                                      type: boolean
                                    runAsUserName:
                                      type: string
                                  type: object
                              type: object
                            startupProbe:
                              properties:
                                exec:
                                  properties:
                                    command:
                                      items:
                                        type: string
                                      type: array
                                  type: object
                                failureThreshold:
                                  format: int32
                                  type: integer
                                grpc:
                                  properties:
                                    port:
                                      format: int32
                                      type: integer
                                    service:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                httpGet:
                                  properties:
                                    host:
                                      type: string
                                    httpHeaders:
                                      items:
                                        properties:
                                          name:
                                            type: string
                                          value:
                                            type: string
                                        required:
                                        - name
                                        - value
                                        type: object
                                      type: array
                                    path:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                    scheme:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                initialDelaySeconds:
                                  format: int32
                                  type: integer
                                periodSeconds:
                                  format: int32
                                  type: integer
                                successThreshold:
                                  format: int32
                                  type: integer
                                tcpSocket:
                                  properties:
                                    host:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                  required:
                                  - port
                                  type: object
                                terminationGracePeriodSeconds:
                                  format: int64
                                  type: integer
                                timeoutSeconds:
                                  format: int32
                                  type: integer
                              type: object
                            stdin:
                              type: boolean
                            stdinOnce:
                              type: boolean
                            terminationMessagePath:
                              type: string
                            terminationMessagePolicy:
                              type: string
                            tty:
                              type: boolean
                            volumeDevices:
                              items:
                                properties:
                                  devicePath:
                                    type: string
                                  name:
                                    type: string
                                required:
                                - devicePath
                                - name
                                type: object
                              type: array
                            volumeMounts:
                              items:
                                properties:
                                  mountPath:
                                    type: string
                                  mountPropagation:
                                    type: string
                                  name:
                                    type: string
                                  readOnly:
                                    type: boolean
                                  subPath:
                                    type: string
                                  subPathExpr:
                                    type: string
                                required:
                                - mountPath
                                - name
                                type: object
                              type: array
                            workingDir:
                              type: string
                          required:
                          - name
                          type: object
                        type: array
                      dnsConfig:
                        properties:
                          nameservers:
                            items:
                              type: string
                            type: array
                          options:
                            items:
                              properties:
                                name:
                                  type: string
                                value:
                                  type: string
                              type: object
                            type: array
                          searches:
                            items:
                              type: string
                            type: array
                        type: object
                      dnsPolicy:
                        type: string
                      enableServiceLinks:
                        type: boolean
                      ephemeralContainers:
                        items:
                          properties:
                            args:
                              items:
                                type: string
                              type: array
                            command:
                              items:
                                type: string
                              type: array
                            env:
                              items:
                                properties:
                                  name:
                                    type: string
                                  value:
                                    type: string
                                  valueFrom:
                                    properties:
                                      configMapKeyRef:
                                        properties:
                                          key:
                                            type: string
                                          name:
                                            type: string
                                          optional:
                                            type: boolean
                                        required:
                                        - key
                                        type: object
                                        x-kubernetes-map-type: atomic
                                      fieldRef:
                                        properties:
                                          apiVersion:
                                            type: string
                                          fieldPath:
                                            type: string
                                        required:
                                        - fieldPath
                                        type: object
                                        x-kubernetes-map-type: atomic
                                      resourceFieldRef:
                                        properties:
                                          containerName:
                                            type: string
                                          divisor:
                                            anyOf:
                                            - type: integer
                                            - type: string
                                            pattern: ^(\+|-)?(([0-9[]+(\.[0-9[]*)?)|(\.[0-9[]+))(([KMGTPE[]i)|[numkMGTPE[]|([eE[](\+|-)?(([0-9[]+(\.[0-9[]*)?)|(\.[0-9[]+))))?$
                                            x-kubernetes-int-or-string: true
                                          resource:
                                            type: string
                                        required:
                                        - resource
                                        type: object
                                        x-kubernetes-map-type: atomic
                                      secretKeyRef:
                                        properties:
                                          key:
                                            type: string
                                          name:
                                            type: string
                                          optional:
                                            type: boolean
                                        required:
                                        - key
                                        type: object
                                        x-kubernetes-map-type: atomic
                                    type: object
                                required:
                                - name
                                type: object
                              type: array
                            envFrom:
                              items:
                                properties:
                                  configMapRef:
                                    properties:
                                      name:
                                        type: string
                                      optional:
                                        type: boolean
                                    type: object
                                    x-kubernetes-map-type: atomic
                                  prefix:
                                    type: string
                                  secretRef:
                                    properties:
                                      name:
                                        type: string
                                      optional:
                                        type: boolean
                                    type: object
                                    x-kubernetes-map-type: atomic
                                type: object
                              type: array
                            image:
                              type: string
                            imagePullPolicy:
                              type: string
                            lifecycle:
                              properties:
                                postStart:
                                  properties:
                                    exec:
                                      properties:
                                        command:
                                          items:
                                            type: string
                                          type: array
                                      type: object
                                    httpGet:
                                      properties:
                                        host:
                                          type: string
                                        httpHeaders:
                                          items:
                                            properties:
                                              name:
                                                type: string
                                              value:
                                                type: string
                                            required:
                                            - name
                                            - value
                                            type: object
                                          type: array
                                        path:
                                          type: string
                                        port:
                                          anyOf:
                                          - type: integer
                                          - type: string
                                          x-kubernetes-int-or-string: true
                                        scheme:
                                          type: string
                                      required:
                                      - port
                                      type: object
                                    sleep:
                                      properties:
                                        seconds:
                                          format: int64
                                          type: integer
                                      required:
                                      - seconds
                                      type: object
                                    tcpSocket:
                                      properties:
                                        host:
                                          type: string
                                        port:
                                          anyOf:
                                          - type: integer
                                          - type: string
                                          x-kubernetes-int-or-string: true
                                      required:
                                      - port
                                      type: object
                                  type: object
                                preStop:
                                  properties:
                                    exec:
                                      properties:
                                        command:
                                          items:
                                            type: string
                                          type: array
                                      type: object
                                    httpGet:
                                      properties:
                                        host:
                                          type: string
                                        httpHeaders:
                                          items:
                                            properties:
                                              name:
                                                type: string
                                              value:
                                                type: string
                                            required:
                                            - name
                                            - value
                                            type: object
                                          type: array
                                        path:
                                          type: string
                                        port:
                                          anyOf:
                                          - type: integer
                                          - type: string
                                          x-kubernetes-int-or-string: true
                                        scheme:
                                          type: string
                                      required:
                                      - port
                                      type: object
                                    sleep:
                                      properties:
                                        seconds:
                                          format: int64
                                          type: integer
                                      required:
                                      - seconds
                                      type: object
                                    tcpSocket:
                                      properties:
                                        host:
                                          type: string
                                        port:
                                          anyOf:
                                          - type: integer
                                          - type: string
                                          x-kubernetes-int-or-string: true
                                      required:
                                      - port
                                      type: object
                                  type: object
                              type: object
                            livenessProbe:
                              properties:
                                exec:
                                  properties:
                                    command:
                                      items:
                                        type: string
                                      type: array
                                  type: object
                                failureThreshold:
                                  format: int32
                                  type: integer
                                grpc:
                                  properties:
                                    port:
                                      format: int32
                                      type: integer
                                    service:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                httpGet:
                                  properties:
                                    host:
                                      type: string
                                    httpHeaders:
                                      items:
                                        properties:
                                          name:
                                            type: string
                                          value:
                                            type: string
                                        required:
                                        - name
                                        - value
                                        type: object
                                      type: array
                                    path:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                    scheme:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                initialDelaySeconds:
                                  format: int32
                                  type: integer
                                periodSeconds:
                                  format: int32
                                  type: integer
                                successThreshold:
                                  format: int32
                                  type: integer
                                tcpSocket:
                                  properties:
                                    host:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                  required:
                                  - port
                                  type: object
                                terminationGracePeriodSeconds:
                                  format: int64
                                  type: integer
                                timeoutSeconds:
                                  format: int32
                                  type: integer
                              type: object
                            name:
                              type: string
                            ports:
                              items:
                                properties:
                                  containerPort:
                                    format: int32
                                    type: integer
                                  hostIP:
                                    type: string
                                  hostPort:
                                    format: int32
                                    type: integer
                                  name:
                                    type: string
                                  protocol:
                                    default: TCP
                                    type: string
                                required:
                                - containerPort
                                type: object
                              type: array
                              x-kubernetes-list-map-keys:
                              - containerPort
                              - protocol
                              x-kubernetes-list-type: map
                            readinessProbe:
                              properties:
                                exec:
                                  properties:
                                    command:
                                      items:
                                        type: string
                                      type: array
                                  type: object
                                failureThreshold:
                                  format: int32
                                  type: integer
                                grpc:
                                  properties:
                                    port:
                                      format: int32
                                      type: integer
                                    service:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                httpGet:
                                  properties:
                                    host:
                                      type: string
                                    httpHeaders:
                                      items:
                                        properties:
                                          name:
                                            type: string
                                          value:
                                            type: string
                                        required:
                                        - name
                                        - value
                                        type: object
                                      type: array
                                    path:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                    scheme:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                initialDelaySeconds:
                                  format: int32
                                  type: integer
                                periodSeconds:
                                  format: int32
                                  type: integer
                                successThreshold:
                                  format: int32
                                  type: integer
                                tcpSocket:
                                  properties:
                                    host:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                  required:
                                  - port
                                  type: object
                                terminationGracePeriodSeconds:
                                  format: int64
                                  type: integer
                                timeoutSeconds:
                                  format: int32
                                  type: integer
                              type: object
                            resizePolicy:
                              items:
                                properties:
                                  resourceName:
                                    type: string
                                  restartPolicy:
                                    type: string
                                required:
                                - resourceName
                                - restartPolicy
                                type: object
                              type: array
                              x-kubernetes-list-type: atomic
                            resources:
                              properties:
                                claims:
                                  items:
                                    properties:
                                      name:
                                        type: string
                                    required:
                                    - name
                                    type: object
                                  type: array
                                  x-kubernetes-list-map-keys:
                                  - name
                                  x-kubernetes-list-type: map
                                limits:
                                  x-kubernetes-preserve-unknown-fields: true
                                requests:
                                  x-kubernetes-preserve-unknown-fields: true
                              type: object
                            restartPolicy:
                              type: string
                            securityContext:
                              properties:
                                allowPrivilegeEscalation:
                                  type: boolean
                                capabilities:
                                  properties:
                                    add:
                                      items:
                                        type: string
                                      type: array
                                    drop:
                                      items:
                                        type: string
                                      type: array
                                  type: object
                                privileged:
                                  type: boolean
                                procMount:
                                  type: string
                                readOnlyRootFilesystem:
                                  type: boolean
                                runAsGroup:
                                  format: int64
                                  type: integer
                                runAsNonRoot:
                                  type: boolean
                                runAsUser:
                                  format: int64
                                  type: integer
                                seLinuxOptions:
                                  properties:
                                    level:
                                      type: string
                                    role:
                                      type: string
                                    type:
                                      type: string
                                    user:
                                      type: string
                                  type: object
                                seccompProfile:
                                  properties:
                                    localhostProfile:
                                      type: string
                                    type:
                                      type: string
                                  required:
                                  - type
                                  type: object
                                windowsOptions:
                                  properties:
                                    gmsaCredentialSpec:
                                      type: string
                                    gmsaCredentialSpecName:
                                      type: string
                                    hostProcess:
                                      type: boolean
                                    runAsUserName:
                                      type: string
                                  type: object
                              type: object
                            startupProbe:
                              properties:
                                exec:
                                  properties:
                                    command:
                                      items:
                                        type: string
                                      type: array
                                  type: object
                                failureThreshold:
                                  format: int32
                                  type: integer
                                grpc:
                                  properties:
                                    port:
                                      format: int32
                                      type: integer
                                    service:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                httpGet:
                                  properties:
                                    host:
                                      type: string
                                    httpHeaders:
                                      items:
                                        properties:
                                          name:
                                            type: string
                                          value:
                                            type: string
                                        required:
                                        - name
                                        - value
                                        type: object
                                      type: array
                                    path:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                    scheme:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                initialDelaySeconds:
                                  format: int32
                                  type: integer
                                periodSeconds:
                                  format: int32
                                  type: integer
                                successThreshold:
                                  format: int32
                                  type: integer
                                tcpSocket:
                                  properties:
                                    host:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                  required:
                                  - port
                                  type: object
                                terminationGracePeriodSeconds:
                                  format: int64
                                  type: integer
                                timeoutSeconds:
                                  format: int32
                                  type: integer
                              type: object
                            stdin:
                              type: boolean
                            stdinOnce:
                              type: boolean
                            targetContainerName:
                              type: string
                            terminationMessagePath:
                              type: string
                            terminationMessagePolicy:
                              type: string
                            tty:
                              type: boolean
                            volumeDevices:
                              items:
                                properties:
                                  devicePath:
                                    type: string
                                  name:
                                    type: string
                                required:
                                - devicePath
                                - name
                                type: object
                              type: array
                            volumeMounts:
                              items:
                                properties:
                                  mountPath:
                                    type: string
                                  mountPropagation:
                                    type: string
                                  name:
                                    type: string
                                  readOnly:
                                    type: boolean
                                  subPath:
                                    type: string
                                  subPathExpr:
                                    type: string
                                required:
                                - mountPath
                                - name
                                type: object
                              type: array
                            workingDir:
                              type: string
                          required:
                          - name
                          type: object
                        type: array
                      hostAliases:
                        items:
                          properties:
                            hostnames:
                              items:
                                type: string
                              type: array
                            ip:
                              type: string
                          type: object
                        type: array
                      hostIPC:
                        type: boolean
                      hostNetwork:
                        type: boolean
                      hostPID:
                        type: boolean
                      hostUsers:
                        type: boolean
                      hostname:
                        type: string
                      imagePullSecrets:
                        items:
                          properties:
                            name:
                              type: string
                          type: object
                          x-kubernetes-map-type: atomic
                        type: array
                      initContainers:
                        items:
                          properties:
                            args:
                              items:
                                type: string
                              type: array
                            command:
                              items:
                                type: string
                              type: array
                            env:
                              items:
                                properties:
                                  name:
                                    type: string
                                  value:
                                    type: string
                                  valueFrom:
                                    properties:
                                      configMapKeyRef:
                                        properties:
                                          key:
                                            type: string
                                          name:
                                            type: string
                                          optional:
                                            type: boolean
                                        required:
                                        - key
                                        type: object
                                        x-kubernetes-map-type: atomic
                                      fieldRef:
                                        properties:
                                          apiVersion:
                                            type: string
                                          fieldPath:
                                            type: string
                                        required:
                                        - fieldPath
                                        type: object
                                        x-kubernetes-map-type: atomic
                                      resourceFieldRef:
                                        properties:
                                          containerName:
                                            type: string
                                          divisor:
                                            anyOf:
                                            - type: integer
                                            - type: string
                                            pattern: ^(\+|-)?(([0-9[]+(\.[0-9[]*)?)|(\.[0-9[]+))(([KMGTPE[]i)|[numkMGTPE[]|([eE[](\+|-)?(([0-9[]+(\.[0-9[]*)?)|(\.[0-9[]+))))?$
                                            x-kubernetes-int-or-string: true
                                          resource:
                                            type: string
                                        required:
                                        - resource
                                        type: object
                                        x-kubernetes-map-type: atomic
                                      secretKeyRef:
                                        properties:
                                          key:
                                            type: string
                                          name:
                                            type: string
                                          optional:
                                            type: boolean
                                        required:
                                        - key
                                        type: object
                                        x-kubernetes-map-type: atomic
                                    type: object
                                required:
                                - name
                                type: object
                              type: array
                            envFrom:
                              items:
                                properties:
                                  configMapRef:
                                    properties:
                                      name:
                                        type: string
                                      optional:
                                        type: boolean
                                    type: object
                                    x-kubernetes-map-type: atomic
                                  prefix:
                                    type: string
                                  secretRef:
                                    properties:
                                      name:
                                        type: string
                                      optional:
                                        type: boolean
                                    type: object
                                    x-kubernetes-map-type: atomic
                                type: object
                              type: array
                            image:
                              type: string
                            imagePullPolicy:
                              type: string
                            lifecycle:
                              properties:
                                postStart:
                                  properties:
                                    exec:
                                      properties:
                                        command:
                                          items:
                                            type: string
                                          type: array
                                      type: object
                                    httpGet:
                                      properties:
                                        host:
                                          type: string
                                        httpHeaders:
                                          items:
                                            properties:
                                              name:
                                                type: string
                                              value:
                                                type: string
                                            required:
                                            - name
                                            - value
                                            type: object
                                          type: array
                                        path:
                                          type: string
                                        port:
                                          anyOf:
                                          - type: integer
                                          - type: string
                                          x-kubernetes-int-or-string: true
                                        scheme:
                                          type: string
                                      required:
                                      - port
                                      type: object
                                    sleep:
                                      properties:
                                        seconds:
                                          format: int64
                                          type: integer
                                      required:
                                      - seconds
                                      type: object
                                    tcpSocket:
                                      properties:
                                        host:
                                          type: string
                                        port:
                                          anyOf:
                                          - type: integer
                                          - type: string
                                          x-kubernetes-int-or-string: true
                                      required:
                                      - port
                                      type: object
                                  type: object
                                preStop:
                                  properties:
                                    exec:
                                      properties:
                                        command:
                                          items:
                                            type: string
                                          type: array
                                      type: object
                                    httpGet:
                                      properties:
                                        host:
                                          type: string
                                        httpHeaders:
                                          items:
                                            properties:
                                              name:
                                                type: string
                                              value:
                                                type: string
                                            required:
                                            - name
                                            - value
                                            type: object
                                          type: array
                                        path:
                                          type: string
                                        port:
                                          anyOf:
                                          - type: integer
                                          - type: string
                                          x-kubernetes-int-or-string: true
                                        scheme:
                                          type: string
                                      required:
                                      - port
                                      type: object
                                    sleep:
                                      properties:
                                        seconds:
                                          format: int64
                                          type: integer
                                      required:
                                      - seconds
                                      type: object
                                    tcpSocket:
                                      properties:
                                        host:
                                          type: string
                                        port:
                                          anyOf:
                                          - type: integer
                                          - type: string
                                          x-kubernetes-int-or-string: true
                                      required:
                                      - port
                                      type: object
                                  type: object
                              type: object
                            livenessProbe:
                              properties:
                                exec:
                                  properties:
                                    command:
                                      items:
                                        type: string
                                      type: array
                                  type: object
                                failureThreshold:
                                  format: int32
                                  type: integer
                                grpc:
                                  properties:
                                    port:
                                      format: int32
                                      type: integer
                                    service:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                httpGet:
                                  properties:
                                    host:
                                      type: string
                                    httpHeaders:
                                      items:
                                        properties:
                                          name:
                                            type: string
                                          value:
                                            type: string
                                        required:
                                        - name
                                        - value
                                        type: object
                                      type: array
                                    path:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                    scheme:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                initialDelaySeconds:
                                  format: int32
                                  type: integer
                                periodSeconds:
                                  format: int32
                                  type: integer
                                successThreshold:
                                  format: int32
                                  type: integer
                                tcpSocket:
                                  properties:
                                    host:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                  required:
                                  - port
                                  type: object
                                terminationGracePeriodSeconds:
                                  format: int64
                                  type: integer
                                timeoutSeconds:
                                  format: int32
                                  type: integer
                              type: object
                            name:
                              type: string
                            ports:
                              items:
                                properties:
                                  containerPort:
                                    format: int32
                                    type: integer
                                  hostIP:
                                    type: string
                                  hostPort:
                                    format: int32
                                    type: integer
                                  name:
                                    type: string
                                  protocol:
                                    default: TCP
                                    type: string
                                required:
                                - containerPort
                                type: object
                              type: array
                              x-kubernetes-list-map-keys:
                              - containerPort
                              - protocol
                              x-kubernetes-list-type: map
                            readinessProbe:
                              properties:
                                exec:
                                  properties:
                                    command:
                                      items:
                                        type: string
                                      type: array
                                  type: object
                                failureThreshold:
                                  format: int32
                                  type: integer
                                grpc:
                                  properties:
                                    port:
                                      format: int32
                                      type: integer
                                    service:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                httpGet:
                                  properties:
                                    host:
                                      type: string
                                    httpHeaders:
                                      items:
                                        properties:
                                          name:
                                            type: string
                                          value:
                                            type: string
                                        required:
                                        - name
                                        - value
                                        type: object
                                      type: array
                                    path:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                    scheme:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                initialDelaySeconds:
                                  format: int32
                                  type: integer
                                periodSeconds:
                                  format: int32
                                  type: integer
                                successThreshold:
                                  format: int32
                                  type: integer
                                tcpSocket:
                                  properties:
                                    host:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                  required:
                                  - port
                                  type: object
                                terminationGracePeriodSeconds:
                                  format: int64
                                  type: integer
                                timeoutSeconds:
                                  format: int32
                                  type: integer
                              type: object
                            resizePolicy:
                              items:
                                properties:
                                  resourceName:
                                    type: string
                                  restartPolicy:
                                    type: string
                                required:
                                - resourceName
                                - restartPolicy
                                type: object
                              type: array
                              x-kubernetes-list-type: atomic
                            resources:
                              properties:
                                claims:
                                  items:
                                    properties:
                                      name:
                                        type: string
                                    required:
                                    - name
                                    type: object
                                  type: array
                                  x-kubernetes-list-map-keys:
                                  - name
                                  x-kubernetes-list-type: map
                                limits:
                                  x-kubernetes-preserve-unknown-fields: true
                                requests:
                                  x-kubernetes-preserve-unknown-fields: true
                              type: object
                            restartPolicy:
                              type: string
                            securityContext:
                              properties:
                                allowPrivilegeEscalation:
                                  type: boolean
                                capabilities:
                                  properties:
                                    add:
                                      items:
                                        type: string
                                      type: array
                                    drop:
                                      items:
                                        type: string
                                      type: array
                                  type: object
                                privileged:
                                  type: boolean
                                procMount:
                                  type: string
                                readOnlyRootFilesystem:
                                  type: boolean
                                runAsGroup:
                                  format: int64
                                  type: integer
                                runAsNonRoot:
                                  type: boolean
                                runAsUser:
                                  format: int64
                                  type: integer
                                seLinuxOptions:
                                  properties:
                                    level:
                                      type: string
                                    role:
                                      type: string
                                    type:
                                      type: string
                                    user:
                                      type: string
                                  type: object
                                seccompProfile:
                                  properties:
                                    localhostProfile:
                                      type: string
                                    type:
                                      type: string
                                  required:
                                  - type
                                  type: object
                                windowsOptions:
                                  properties:
                                    gmsaCredentialSpec:
                                      type: string
                                    gmsaCredentialSpecName:
                                      type: string
                                    hostProcess:
                                      type: boolean
                                    runAsUserName:
                                      type: string
                                  type: object
                              type: object
                            startupProbe:
                              properties:
                                exec:
                                  properties:
                                    command:
                                      items:
                                        type: string
                                      type: array
                                  type: object
                                failureThreshold:
                                  format: int32
                                  type: integer
                                grpc:
                                  properties:
                                    port:
                                      format: int32
                                      type: integer
                                    service:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                httpGet:
                                  properties:
                                    host:
                                      type: string
                                    httpHeaders:
                                      items:
                                        properties:
                                          name:
                                            type: string
                                          value:
                                            type: string
                                        required:
                                        - name
                                        - value
                                        type: object
                                      type: array
                                    path:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                    scheme:
                                      type: string
                                  required:
                                  - port
                                  type: object
                                initialDelaySeconds:
                                  format: int32
                                  type: integer
                                periodSeconds:
                                  format: int32
                                  type: integer
                                successThreshold:
                                  format: int32
                                  type: integer
                                tcpSocket:
                                  properties:
                                    host:
                                      type: string
                                    port:
                                      anyOf:
                                      - type: integer
                                      - type: string
                                      x-kubernetes-int-or-string: true
                                  required:
                                  - port
                                  type: object
                                terminationGracePeriodSeconds:
                                  format: int64
                                  type: integer
                                timeoutSeconds:
                                  format: int32
                                  type: integer
                              type: object
                            stdin:
                              type: boolean
                            stdinOnce:
                              type: boolean
                            terminationMessagePath:
                              type: string
                            terminationMessagePolicy:
                              type: string
                            tty:
                              type: boolean
                            volumeDevices:
                              items:
                                properties:
                                  devicePath:
                                    type: string
                                  name:
                                    type: string
                                required:
                                - devicePath
                                - name
                                type: object
                              type: array
                            volumeMounts:
                              items:
                                properties:
                                  mountPath:
                                    type: string
                                  mountPropagation:
                                    type: string
                                  name:
                                    type: string
                                  readOnly:
                                    type: boolean
                                  subPath:
                                    type: string
                                  subPathExpr:
                                    type: string
                                required:
                                - mountPath
                                - name
                                type: object
                              type: array
                            workingDir:
                              type: string
                          required:
                          - name
                          type: object
                        type: array
                      nodeName:
                        type: string
                      nodeSelector:
                        additionalProperties:
                          type: string
                        type: object
                        x-kubernetes-map-type: atomic
                      os:
                        properties:
                          name:
                            type: string
                        required:
                        - name
                        type: object
                      overhead:
                        additionalProperties:
                          anyOf:
                          - type: integer
                          - type: string
                          pattern: ^(\+|-)?(([0-9[]+(\.[0-9[]*)?)|(\.[0-9[]+))(([KMGTPE[]i)|[numkMGTPE[]|([eE[](\+|-)?(([0-9[]+(\.[0-9[]*)?)|(\.[0-9[]+))))?$
                          x-kubernetes-int-or-string: true
                        type: object
                      preemptionPolicy:
                        type: string
                      priority:
                        format: int32
                        type: integer
                      priorityClassName:
                        type: string
                      readinessGates:
                        items:
                          properties:
                            conditionType:
                              type: string
                          required:
                          - conditionType
                          type: object
                        type: array
                      resourceClaims:
                        items:
                          properties:
                            name:
                              type: string
                            source:
                              properties:
                                resourceClaimName:
                                  type: string
                                resourceClaimTemplateName:
                                  type: string
                              type: object
                          required:
                          - name
                          type: object
                        type: array
                        x-kubernetes-list-map-keys:
                        - name
                        x-kubernetes-list-type: map
                      restartPolicy:
                        type: string
                      runtimeClassName:
                        type: string
                      schedulerName:
                        type: string
                      schedulingGates:
                        items:
                          properties:
                            name:
                              type: string
                          required:
                          - name
                          type: object
                        type: array
                        x-kubernetes-list-map-keys:
                        - name
                        x-kubernetes-list-type: map
                      securityContext:
                        properties:
                          fsGroup:
                            format: int64
                            type: integer
                          fsGroupChangePolicy:
                            type: string
                          runAsGroup:
                            format: int64
                            type: integer
                          runAsNonRoot:
                            type: boolean
                          runAsUser:
                            format: int64
                            type: integer
                          seLinuxOptions:
                            properties:
                              level:
                                type: string
                              role:
                                type: string
                              type:
                                type: string
                              user:
                                type: string
                            type: object
                          seccompProfile:
                            properties:
                              localhostProfile:
                                type: string
                              type:
                                type: string
                            required:
                            - type
                            type: object
                          supplementalGroups:
                            items:
                              format: int64
                              type: integer
                            type: array
                          sysctls:
                            items:
                              properties:
                                name:
                                  type: string
                                value:
                                  type: string
                              required:
                              - name
                              - value
                              type: object
                            type: array
                          windowsOptions:
                            properties:
                              gmsaCredentialSpec:
                                type: string
                              gmsaCredentialSpecName:
                                type: string
                              hostProcess:
                                type: boolean
                              runAsUserName:
                                type: string
                            type: object
                        type: object
                      serviceAccount:
                        type: string
                      serviceAccountName:
                        type: string
                      setHostnameAsFQDN:
                        type: boolean
                      shareProcessNamespace:
                        type: boolean
                      subdomain:
                        type: string
                      terminationGracePeriodSeconds:
                        format: int64
                        type: integer
                      tolerations:
                        items:
                          properties:
                            effect:
                              type: string
                            key:
                              type: string
                            operator:
                              type: string
                            tolerationSeconds:
                              format: int64
                              type: integer
                            value:
                              type: string
                          type: object
                        type: array
                      topologySpreadConstraints:
                        items:
                          properties:
                            labelSelector:
                              properties:
                                matchExpressions:
                                  items:
                                    properties:
                                      key:
                                        type: string
                                      operator:
                                        type: string
                                      values:
                                        items:
                                          type: string
                                        type: array
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                matchLabels:
                                  additionalProperties:
                                    type: string
                                  type: object
                              type: object
                              x-kubernetes-map-type: atomic
                            matchLabelKeys:
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            maxSkew:
                              format: int32
                              type: integer
                            minDomains:
                              format: int32
                              type: integer
                            nodeAffinityPolicy:
                              type: string
                            nodeTaintsPolicy:
                              type: string
                            topologyKey:
                              type: string
                            whenUnsatisfiable:
                              type: string
                          required:
                          - maxSkew
                          - topologyKey
                          - whenUnsatisfiable
                          type: object
                        type: array
                        x-kubernetes-list-map-keys:
                        - topologyKey
                        - whenUnsatisfiable
                        x-kubernetes-list-type: map
                      volumes:
                        items:
                          x-kubernetes-preserve-unknown-fields: true
                        type: array
                    required:
                    - containers
                    type: object
                type: object
              workloadRef:
                properties:
                  apiVersion:
                    type: string
                  kind:
                    type: string
                  name:
                    type: string
                  scaleDown:
                    type: string
                type: object
            type: object
          status:
            properties:
              HPAReplicas:
                format: int32
                type: integer
              abort:
                type: boolean
              abortedAt:
                format: date-time
                type: string
              alb:
                properties:
                  canaryTargetGroup:
                    properties:
                      arn:
                        type: string
                      fullName:
                        type: string
                      name:
                        type: string
                    required:
                    - arn
                    - name
                    type: object
                  ingress:
                    type: string
                  loadBalancer:
                    properties:
                      arn:
                        type: string
                      fullName:
                        type: string
                      name:
                        type: string
                    required:
                    - arn
                    - name
                    type: object
                  stableTargetGroup:
                    properties:
                      arn:
                        type: string
                      fullName:
                        type: string
                      name:
                        type: string
                    required:
                    - arn
                    - name
                    type: object
                type: object
              albs:
                items:
                  properties:
                    canaryTargetGroup:
                      properties:
                        arn:
                          type: string
                        fullName:
                          type: string
                        name:
                          type: string
                      required:
                      - arn
                      - name
                      type: object
                    ingress:
                      type: string
                    loadBalancer:
                      properties:
                        arn:
                          type: string
                        fullName:
                          type: string
                        name:
                          type: string
                      required:
                      - arn
                      - name
                      type: object
                    stableTargetGroup:
                      properties:
                        arn:
                          type: string
                        fullName:
                          type: string
                        name:
                          type: string
                      required:
                      - arn
                      - name
                      type: object
                  type: object
                type: array
              availableReplicas:
                format: int32
                type: integer
              blueGreen:
                properties:
                  activeSelector:
                    type: string
                  postPromotionAnalysisRunStatus:
                    properties:
                      message:
                        type: string
                      name:
                        type: string
                      status:
                        type: string
                    required:
                    - name
                    - status
                    type: object
                  prePromotionAnalysisRunStatus:
                    properties:
                      message:
                        type: string
                      name:
                        type: string
                      status:
                        type: string
                    required:
                    - name
                    - status
                    type: object
                  previewSelector:
                    type: string
                  scaleUpPreviewCheckPoint:
                    type: boolean
                type: object
              canary:
                properties:
                  currentBackgroundAnalysisRunStatus:
                    properties:
                      message:
                        type: string
                      name:
                        type: string
                      status:
                        type: string
                    required:
                    - name
                    - status
                    type: object
                  currentExperiment:
                    type: string
                  currentStepAnalysisRunStatus:
                    properties:
                      message:
                        type: string
                      name:
                        type: string
                      status:
                        type: string
                    required:
                    - name
                    - status
                    type: object
                  stablePingPong:
                    type: string
                  weights:
                    properties:
                      additional:
                        items:
                          properties:
                            podTemplateHash:
                              type: string
                            serviceName:
                              type: string
                            weight:
                              format: int32
                              type: integer
                          required:
                          - weight
                          type: object
                        type: array
                      canary:
                        properties:
                          podTemplateHash:
                            type: string
                          serviceName:
                            type: string
                          weight:
                            format: int32
                            type: integer
                        required:
                        - weight
                        type: object
                      stable:
                        properties:
                          podTemplateHash:
                            type: string
                          serviceName:
                            type: string
                          weight:
                            format: int32
                            type: integer
                        required:
                        - weight
                        type: object
                      verified:
                        type: boolean
                    required:
                    - canary
                    - stable
                    type: object
                type: object
              collisionCount:
                format: int32
                type: integer
              conditions:
                items:
                  properties:
                    lastTransitionTime:
                      format: date-time
                      type: string
                    lastUpdateTime:
                      format: date-time
                      type: string
                    message:
                      type: string
                    reason:
                      type: string
                    status:
                      type: string
                    type:
                      type: string
                  required:
                  - lastTransitionTime
                  - lastUpdateTime
                  - message
                  - reason
                  - status
                  - type
                  type: object
                type: array
              controllerPause:
                type: boolean
              currentPodHash:
                type: string
              currentStepHash:
                type: string
              currentStepIndex:
                format: int32
                type: integer
              message:
                type: string
              observedGeneration:
                type: string
              pauseConditions:
                items:
                  properties:
                    reason:
                      type: string
                    startTime:
                      format: date-time
                      type: string
                  required:
                  - reason
                  - startTime
                  type: object
                type: array
              phase:
                type: string
              promoteFull:
                type: boolean
              readyReplicas:
                format: int32
                type: integer
              replicas:
                format: int32
                type: integer
              restartedAt:
                format: date-time
                type: string
              selector:
                type: string
              stableRS:
                type: string
              updatedReplicas:
                format: int32
                type: integer
              workloadObservedGeneration:
                type: string
            type: object
        required:
        - spec
        type: object
    served: true
    storage: true
    subresources:
      scale:
        labelSelectorPath: .status.selector
        specReplicasPath: .spec.replicas
        statusReplicasPath: .status.HPAReplicas
      status: {}
status:
  acceptedNames:
    kind: Rollout
    listKind: RolloutList
    plural: rollouts
    shortNames:
    - ro
    singular: rollout
  conditions:
  - lastTransitionTime: "2025-02-17T18:46:50Z"
    message: no conflicts found
    reason: NoConflicts
    status: "True"
    type: NamesAccepted
  - lastTransitionTime: "2025-02-17T18:46:50Z"
    message: the initial names have been accepted
    reason: InitialNamesAccepted
    status: "True"
    type: Established
  storedVersions:
  - v1alpha1

# Examples

UQ:
  "Create a rollout using the Kubernetes Gateway API for a new version of the rollouts-demo application with a canary strategy based on the http-benchmark analysis run template."
JSON:
  {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "Rollout",
    "metadata": {
      "name": "rollouts-demo",
      "namespace": "default"
    },
    "spec": {
      "replicas": 3,
      "strategy": {
        "canary": {
          "canaryService": "rollouts-demo-canary",
          "stableService": "rollouts-demo-stable",
          "trafficRouting": {
            "plugins": {
              "argoproj-labs/gatewayAPI": {
                "httpRoute": "argo-rollouts-http-route",
                "namespace": "default"
              }
            }
          },
          "analysis": {
            "startingStep": 1,
            "templates": [
              {
                "templateName": "http-benchmark"
              }
            ],
            "args": [
              {
                "name": "host",
                "value": "rollouts-demo-canary"
              }
            ]
          },
          "steps": [
            {
              "setWeight": 30
            },
            {
              "pause": {
                "duration": "30s"
              }
            },
            {
              "setWeight": 60
            },
            {
              "pause": {
                "duration": "30s"
              }
            },
            {
              "setWeight": 100
            },
            {
              "pause": {
                "duration": "30s"
              }
            }
          ]
        }
      },
      "revisionHistoryLimit": 2,
      "selector": {
        "matchLabels": {
          "app": "rollouts-demo"
        }
      },
      "template": {
        "metadata": {
          "labels": {
            "app": "rollouts-demo"
          }
        },
        "spec": {
          "containers": [
            {
              "name": "rollouts-demo",
              "image": "argoproj/rollouts-demo:orange",
              "ports": [
                {
                  "name": "http",
                  "containerPort": 8080,
                  "protocol": "TCP"
                }
              ],
              "resources": {
                "requests": {
                  "memory": "32Mi",
                  "cpu": "5m"
                }
              }
            }
          ]
        }
      }
    }
  }

UQ:
  "Create a rollout using the Kubernetes Gateway API for a new version of the rollouts-demo application with a canary strategy using a green image."
JSON:
  {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "Rollout",
    "metadata": {
      "name": "rollouts-demo",
      "namespace": "default"
    },
    "spec": {
      "replicas": 3,
      "strategy": {
        "canary": {
          "canaryService": "rollouts-demo-canary",
          "stableService": "rollouts-demo-stable",
          "trafficRouting": {
            "plugins": {
              "argoproj-labs/gatewayAPI": {
                "httpRoute": "argo-rollouts-http-route",
                "namespace": "default"
              }
            }
          },
          "steps": [
            {
              "setWeight": 30
            },
            {
              "pause": {
                "duration": "30s"
              }
            },
            {
              "setWeight": 60
            },
            {
              "pause": {
                "duration": "30s"
              }
            },
            {
              "setWeight": 100
            },
            {
              "pause": {
                "duration": "30s"
              }
            }
          ]
        }
      },
      "revisionHistoryLimit": 2,
      "selector": {
        "matchLabels": {
          "app": "rollouts-demo"
        }
      },
      "template": {
        "metadata": {
          "labels": {
            "app": "rollouts-demo"
          }
        },
        "spec": {
          "containers": [
            {
              "name": "rollouts-demo",
              "image": "argoproj/rollouts-demo:green",
              "ports": [
                {
                  "name": "http",
                  "containerPort": 8080,
                  "protocol": "TCP"
                }
              ],
              "resources": {
                "requests": {
                  "memory": "32Mi",
                  "cpu": "5m"
                }
              }
            }
          ]
        }
      }
    }
  }

UQ:
  "Create a rollout for a new version of the rollout-bluegreen application using a blue-green strategy with manual promotion"
JSON:
  {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "Rollout",
    "metadata": {
      "name": "rollout-bluegreen"
    },
    "spec": {
      "replicas": 2,
      "revisionHistoryLimit": 2,
      "selector": {
        "matchLabels": {
          "app": "rollout-bluegreen"
        }
      },
      "template": {
        "metadata": {
          "labels": {
            "app": "rollout-bluegreen"
          }
        },
        "spec": {
          "containers": [
            {
              "name": "rollouts-demo",
              "image": "argoproj/rollouts-demo:blue",
              "imagePullPolicy": "Always",
              "ports": [
                {
                  "containerPort": 8080
                }
              ]
            }
          ]
        }
      },
      "strategy": {
        "blueGreen": {
          "activeService": "rollout-bluegreen-active",
          "previewService": "rollout-bluegreen-preview",
          "autoPromotionEnabled": false
        }
      }
    }
  }

UQ:
  "Create a rollout for the istio-subset-split application using a canary strategy with Istio traffic routing"
JSON:
  {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "Rollout",
    "metadata": {
      "name": "istio-subset-split"
    },
    "spec": {
      "strategy": {
        "canary": {
          "trafficRouting": {
            "istio": {
              "virtualService": {
                "name": "istio-subset-split-vsvc",
                "routes": [
                  "primary"
                ]
              },
              "destinationRule": {
                "name": "istio-subset-split-destrule",
                "canarySubsetName": "canary",
                "stableSubsetName": "stable"
              }
            }
          },
          "steps": [
            {
              "setWeight": 10
            },
            {
              "pause": {}
            }
          ]
        }
      },
      "selector": {
        "matchLabels": {
          "app": "istio-subset-split"
        }
      },
      "template": {
        "metadata": {
          "labels": {
            "app": "istio-subset-split"
          }
        },
        "spec": {
          "containers": [
            {
              "name": "istio-subset-split",
              "image": "nginx:1.19-alpine",
              "ports": [
                {
                  "name": "http",
                  "containerPort": 80,
                  "protocol": "TCP"
                }
              ],
              "resources": {
                "requests": {
                  "memory": "16Mi",
                  "cpu": "5m"
                }
              }
            }
          ]
        }
      }
    }
  }
"""