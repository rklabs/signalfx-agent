package utils

import (
	"errors"
	"reflect"
	"sort"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

// PodServiceCache is an internal cache for mapping
// services to pods for property propagation.
type PodServiceCache struct {
	svcUIDNamespaceCache map[types.UID]string
	svcUIDNameCache      map[types.UID]string
	svcUIDSelectorCache  map[types.UID]labels.Selector
	podUIDLabelCache     map[types.UID]labels.Set
	podUIDNamespaceCache map[types.UID]string
	podSvcUIDCache       map[types.UID]svcSet
	svcPodUIDCache       map[types.UID]podsSet
}

type svcSet map[types.UID]bool

type podsSet map[types.UID]bool

// NewPodServiceCache creates a new service:pod cache
func NewPodServiceCache() *PodServiceCache {
	return &PodServiceCache{
		svcUIDNamespaceCache: make(map[types.UID]string),
		svcUIDNameCache:      make(map[types.UID]string),
		svcUIDSelectorCache:  make(map[types.UID]labels.Selector),
		podUIDLabelCache:     make(map[types.UID]labels.Set),
		podUIDNamespaceCache: make(map[types.UID]string),
		podSvcUIDCache:       make(map[types.UID]svcSet),
		svcPodUIDCache:       make(map[types.UID]podsSet),
	}
}

// SetService attempts to add a new service to the cache or update
// an existing service in the cache. We only really care about the service
// name or selector changing for re-mapping the pod:service relationships.
// If there is an update to a service but neither of these change,
// it is a no-op for us
func (psc *PodServiceCache) SetService(svc *v1.Service) {
	selector := labels.Set(svc.Spec.Selector).AsSelectorPreValidated()
	if selector.Empty() {
		return
	}

	cachedNamespace := psc.svcUIDNamespaceCache[svc.UID]
	cachedSelector := psc.svcUIDSelectorCache[svc.UID]
	cachedName := psc.svcUIDNameCache[svc.UID]

	// if already cached & selector, name, namespace did not change, no-op.
	if (reflect.DeepEqual(cachedSelector, selector)) &&
		(cachedName == svc.Name) && (cachedNamespace == svc.Namespace) {
		return
	}

	psc.svcUIDNamespaceCache[svc.UID] = svc.Namespace
	psc.svcUIDSelectorCache[svc.UID] = selector
	psc.svcUIDNameCache[svc.UID] = svc.Name
	psc.refreshCacheByService(svc)
}

// DeleteServiceFromCache takes a service and removes it from all
// internal caches.
func (psc *PodServiceCache) DeleteServiceFromCache(svcUID types.UID) {
	delete(psc.svcUIDNamespaceCache, svcUID)
	delete(psc.svcUIDSelectorCache, svcUID)
	delete(psc.svcUIDNameCache, svcUID)
	if podsSet, exists := psc.svcPodUIDCache[svcUID]; exists {
		for podUID := range podsSet {
			delete(psc.podSvcUIDCache[podUID], svcUID)
		}
		delete(psc.svcPodUIDCache, svcUID)
	}
}

// SetPod attempts to add a new pod to the cache or update an
// existing one. After a pod is added or updated, we need to check
// cached services to find which ones match
func (psc *PodServiceCache) SetPod(pod *v1.Pod) {
	labelSet := labels.Set(pod.Labels)
	cachedLabelSet := psc.podUIDLabelCache[pod.UID]
	cachedNamespace := psc.podUIDNamespaceCache[pod.UID]

	// if the pod label set didn't change, no-op
	if reflect.DeepEqual(cachedLabelSet, labelSet) &&
		cachedNamespace == pod.Namespace {
		return
	}

	psc.podUIDNamespaceCache[pod.UID] = pod.Namespace
	psc.podUIDLabelCache[pod.UID] = labelSet
	psc.refreshCacheByPod(pod)
}

// DeletePodFromCache takes a pod and removes it from all
// internal caches
func (psc *PodServiceCache) DeletePodFromCache(podUID types.UID) {
	delete(psc.podUIDNamespaceCache, podUID)
	delete(psc.podUIDLabelCache, podUID)
	if servicesSet, exists := psc.podSvcUIDCache[podUID]; exists {
		for svcUID := range servicesSet {
			delete(psc.svcPodUIDCache[svcUID], podUID)
		}
		delete(psc.podSvcUIDCache, podUID)
	}
}

// refreshCacheByService should be called when a service is added
// or updated and the pod:service mappings need to be refreshed.
// This function loops through all pods in the cache and checks if
// any match the given service.
func (psc *PodServiceCache) refreshCacheByService(svc *v1.Service) []types.UID {
	var pods []types.UID
	if selector, exists := psc.svcUIDSelectorCache[svc.UID]; exists {
		for podUID, labelSet := range psc.podUIDLabelCache {
			if selector.Matches(labelSet) &&
				psc.podUIDNamespaceCache[podUID] == svc.Namespace {
				if _, exists := psc.podSvcUIDCache[podUID]; !exists {
					psc.podSvcUIDCache[podUID] = make(map[types.UID]bool)

				}
				if _, exists := psc.svcPodUIDCache[svc.UID]; !exists {
					psc.svcPodUIDCache[svc.UID] = make(map[types.UID]bool)
				}
				pods = append(pods, podUID)
				psc.podSvcUIDCache[podUID][svc.UID] = true
				psc.svcPodUIDCache[svc.UID][podUID] = true
			}
		}
	}
	return pods
}

// refreshCacheByPod should be called when a pod is added
// or updated and the pod:service mappings need to be refreshed.
// This function loops through all services in the cache and checks if
// any match the given pod.
func (psc *PodServiceCache) refreshCacheByPod(pod *v1.Pod) []types.UID {
	var services []types.UID
	if labelSet, exists := psc.podUIDLabelCache[pod.UID]; exists {
		for svcUID, selector := range psc.svcUIDSelectorCache {
			if selector.Matches(labelSet) &&
				psc.svcUIDNamespaceCache[svcUID] == pod.Namespace {
				if _, exists := psc.podSvcUIDCache[pod.UID]; !exists {
					psc.podSvcUIDCache[pod.UID] = make(map[types.UID]bool)

				}
				if _, exists := psc.svcPodUIDCache[svcUID]; !exists {
					psc.svcPodUIDCache[svcUID] = make(map[types.UID]bool)
				}
				services = append(services, svcUID)
				psc.podSvcUIDCache[pod.UID][svcUID] = true
				psc.svcPodUIDCache[svcUID][pod.UID] = true
			}
		}
	}
	return services
}

// GetPodUIDsForService looks up a service in the cache and returns
// the pods that match the services selector.
func (psc *PodServiceCache) GetPodUIDsForService(svc *v1.Service) []types.UID {
	return psc.GetPodUIDsForServiceUID(svc.UID)
}

// GetPodUIDsForServiceUID looks up a service in the cache and returns
// the pods that match the services selector.
func (psc *PodServiceCache) GetPodUIDsForServiceUID(svcUID types.UID) []types.UID {
	var pods []types.UID
	if podSet, exists := psc.svcPodUIDCache[svcUID]; exists {
		for podUID := range podSet {
			pods = append(pods, podUID)
		}
	}
	return pods
}

// getServiceNamesForPod looks up a pod in the cache and returns its matching
// services as a map in the form service_uid: service_name
func (psc *PodServiceCache) getServiceNamesForPod(pod *v1.Pod) []string {
	var services []string
	if serviceUIDs, exists := psc.podSvcUIDCache[pod.UID]; exists {
		for svcUID := range serviceUIDs {
			services = append(services, psc.svcUIDNameCache[svcUID])
		}
	}
	return services
}

// GetServiceNameForPod deterministically returns a single service
// matched by a pods labels. Used by the DataPoint cache for
// updating the "service" property, which only may have one value.
// Selection method is sorting strings alphabetically and selecting the
// first service off the top.
func (psc *PodServiceCache) GetServiceNameForPod(pod *v1.Pod) (string, error) {
	var service string
	services := psc.getServiceNamesForPod(pod)
	if len(services) == 0 {
		return service, errors.New("no service matched pod")
	}
	sort.Strings(services)
	service = services[0]
	return service, nil
}

// getServiceNamesForPod looks up a pod in the cache and returns its matching
// services as a map in the form service_uid: service_name
func (psc *PodServiceCache) getServiceNamesForPodUID(podUID types.UID) []string {
	var services []string
	if serviceUIDs, exists := psc.podSvcUIDCache[podUID]; exists {
		for svcUID := range serviceUIDs {
			services = append(services, psc.svcUIDNameCache[svcUID])
		}
	}
	return services
}

// GetServiceNameForPodUID deterministically returns a single service
// matched by a pods labels. Used by the DataPoint cache for
// updating the "service" property, which only may have one value.
// Selection method is sorting strings alphabetically and selecting the
// first service off the top.
func (psc *PodServiceCache) GetServiceNameForPodUID(podUID types.UID) (string, error) {
	var service string
	services := psc.getServiceNamesForPodUID(podUID)
	if len(services) == 0 {
		return service, errors.New("no service matched pod")
	}
	sort.Strings(services)
	service = services[0]
	return service, nil
}
