package utils

import (
	"reflect"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

type podsSet map[types.UID]bool

// PodCache is used for holding values we care about from a pod
// for quicker lookup than querying the API for them each time.
type PodCache struct {
	namespacePodUIDCache map[string]podsSet
	podUIDNamespaceCache map[types.UID]string
	podUIDLabelCache     map[types.UID]labels.Set
	podUIDORCache        map[types.UID][]metav1.OwnerReference
}

// NewPodCache creates a new minimal pod cache
func NewPodCache() *PodCache {
	return &PodCache{
		namespacePodUIDCache: make(map[string]podsSet),
		podUIDNamespaceCache: make(map[types.UID]string),
		podUIDLabelCache:     make(map[types.UID]labels.Set),
		podUIDORCache:        make(map[types.UID][]metav1.OwnerReference),
	}
}

// IsCached checks if a pod was already in the cache, or if
// the mapped values have changed. Returns true if no change
func (pc *PodCache) IsCached(pod *v1.Pod) bool {
	labelSet := labels.Set(pod.Labels)
	cachedLabelSet := pc.podUIDLabelCache[pod.UID]
	cachedNamespace := pc.podUIDNamespaceCache[pod.UID]
	cachedOR := pc.podUIDORCache[pod.UID]

	return reflect.DeepEqual(cachedLabelSet, labelSet) &&
		(cachedNamespace == pod.Namespace) &&
		(reflect.DeepEqual(cachedOR, pod.OwnerReferences))
}

// AddPod adds or updates a pod in cache
func (pc *PodCache) AddPod(pod *v1.Pod) {
	if _, exists := pc.namespacePodUIDCache[pod.Namespace]; !exists {
		pc.namespacePodUIDCache[pod.Namespace] = make(map[types.UID]bool)
	}
	pc.namespacePodUIDCache[pod.Namespace][pod.UID] = true
	pc.podUIDNamespaceCache[pod.UID] = pod.Namespace
	pc.podUIDLabelCache[pod.UID] = labels.Set(pod.Labels)
	pc.podUIDORCache[pod.UID] = pod.OwnerReferences
}

// DeleteByKey removes a pod from the cache given a UID
func (pc *PodCache) DeleteByKey(key types.UID) {
	namespace := pc.podUIDNamespaceCache[key]
	delete(pc.namespacePodUIDCache[namespace], key)
	delete(pc.podUIDNamespaceCache, key)
	delete(pc.podUIDLabelCache, key)
}

// GetLabels retrieves a pod's cached label set
func (pc *PodCache) GetLabels(key types.UID) labels.Set {
	return pc.podUIDLabelCache[key]
}

// GetOwnerReferences retrieves a pod's cached owner references
func (pc *PodCache) GetOwnerReferences(key types.UID) []metav1.OwnerReference {
	return pc.podUIDORCache[key]
}

// GetPodsInNamespace returns a list of pod UIDs given a namespace
func (pc *PodCache) GetPodsInNamespace(namespace string) []types.UID {
	var pods []types.UID
	if podsSet, exists := pc.namespacePodUIDCache[namespace]; exists {
		for podUID := range podsSet {
			pods = append(pods, podUID)
		}
	}
	return pods
}

// GetMatchingServices returns a list of service names that match the given
// pod, given the services are in the cache arleady
func (pc *PodCache) GetMatchingServices(podUID types.UID, sc *ServiceCache) []string {
	var services []string
	if labelSet, exists := pc.podUIDLabelCache[podUID]; exists {
		for svcUID, selector := range sc.svcUIDSelectorCache {
			if selector.Matches(labelSet) &&
				sc.svcUIDNamespaceCache[svcUID] == pc.podUIDNamespaceCache[podUID] {
				// update service:pods cache
				sc.svcUIDPodsCache[svcUID][podUID] = true
				services = append(services, sc.svcUIDNameCache[svcUID])
			}
		}
	}
	return services
}
