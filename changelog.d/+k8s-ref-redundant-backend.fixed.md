**`k8s://` secret references required a redundant registered backend.**
`Resolver.resolveOne` looked the backend up before the Kubernetes
branch, so a `k8s://` reference failed with "no backend registered for
scheme" unless an operator configured a `k8s` backend that the
Kubernetes-native path then never called. The branch now runs before the
lookup, matching its intent: these references become a `secretKeyRef`
and are never read by the controller.
