package utils

import (
	"reflect"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

// ServiceCache is used for holding values we care about from a pod
// for quicker lookup than querying the API for them each time.
type ServiceCache struct {
	svcUIDNamespaceCache map[types.UID]string
	svcUIDNameCache      map[types.UID]string
	svcUIDSelectorCache  map[types.UID]labels.Selector
	svcUIDPodsCache      map[types.UID]podsSet
}

// NewServiceCache creates a new minimal pod cache
func NewServiceCache() *ServiceCache {
	return &ServiceCache{
		svcUIDNamespaceCache: make(map[types.UID]string),
		svcUIDNameCache:      make(map[types.UID]string),
		svcUIDSelectorCache:  make(map[types.UID]labels.Selector),
		svcUIDPodsCache:      make(map[types.UID]podsSet),
	}
}

// IsCached checks if a service is already in the cache or if even of
// the cached fields have changed.
func (sc *ServiceCache) IsCached(svc *v1.Service) bool {
	selector := labels.Set(svc.Spec.Selector).AsSelectorPreValidated()
	cachedNamespace := sc.svcUIDNamespaceCache[svc.UID]
	cachedSelector := sc.svcUIDSelectorCache[svc.UID]
	cachedName := sc.svcUIDNameCache[svc.UID]

	return (reflect.DeepEqual(cachedSelector, selector)) &&
		(cachedName == svc.Name) && (cachedNamespace == svc.Namespace)

}

// AddService adds or updates a service in cache
func (sc *ServiceCache) AddService(svc *v1.Service) {
	selector := labels.Set(svc.Spec.Selector).AsSelectorPreValidated()
	// empty selectors match nothing
	if selector.Empty() {
		return
	}
	sc.svcUIDPodsCache[svc.UID] = make(map[types.UID]bool)
	sc.svcUIDNamespaceCache[svc.UID] = svc.Namespace
	sc.svcUIDSelectorCache[svc.UID] = selector
	sc.svcUIDNameCache[svc.UID] = svc.Name
}

// Delete removes a service from the cache
func (sc *ServiceCache) Delete(svc *v1.Service) {
	sc.DeleteByKey(svc.UID)
}

// DeleteByKey removes a service from the cache given a UID
// Returns pods that were previously matched by this service
// so their properties may be updated accordingly
func (sc *ServiceCache) DeleteByKey(key types.UID) []types.UID {
	var pods []types.UID
	for podUID := range sc.svcUIDPodsCache[key] {
		pods = append(pods, podUID)
	}
	delete(sc.svcUIDPodsCache, key)
	delete(sc.svcUIDNamespaceCache, key)
	delete(sc.svcUIDSelectorCache, key)
	delete(sc.svcUIDNameCache, key)
	return pods
}
