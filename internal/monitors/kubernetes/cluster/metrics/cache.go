package metrics

import (
	"errors"
	"github.com/davecgh/go-spew/spew"
	"github.com/signalfx/golib/datapoint"
	k8sutil "github.com/signalfx/signalfx-agent/internal/monitors/kubernetes/utils"
	atypes "github.com/signalfx/signalfx-agent/internal/monitors/types"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sync"
)

// ContainerID is some type of unique id for containers
type ContainerID string

var logger = log.WithFields(log.Fields{
	"monitorType": "kubernetes-cluster",
})

// DatapointCache holds an up to date copy of datapoints pertaining to the
// cluster.  It is updated whenever the HandleAdd method is called with new
// K8s resources.
type DatapointCache struct {
	sync.Mutex
	dpCache         map[types.UID][]*datapoint.Datapoint
	dimPropCache    map[types.UID]*atypes.DimProperties
	uidKindCache    map[types.UID]string
	podServiceCache *k8sutil.PodServiceCache
	useNodeName     bool
}

// NewDatapointCache creates a new clean cache
func NewDatapointCache(useNodeName bool) *DatapointCache {
	return &DatapointCache{
		dpCache:         make(map[types.UID][]*datapoint.Datapoint),
		dimPropCache:    make(map[types.UID]*atypes.DimProperties),
		uidKindCache:    make(map[types.UID]string),
		podServiceCache: k8sutil.NewPodServiceCache(),
		useNodeName:     useNodeName,
	}
}

func keyForObject(obj runtime.Object) (types.UID, error) {
	var key types.UID
	oma, ok := obj.(metav1.ObjectMetaAccessor)
	if !ok || oma.GetObjectMeta() == nil {
		return key, errors.New("K8s object is not of the expected form")
	}
	key = oma.GetObjectMeta().GetUID()
	return key, nil
}

// DeleteByKey delete a cache entry by key.  The supplied interface MUST be the
// same type returned by Handle[Add|Delete].  MUST HOLD LOCK!
func (dc *DatapointCache) DeleteByKey(key interface{}) {
	cacheKey := key.(types.UID)

	switch dc.uidKindCache[cacheKey] {
	case "Pod":
		dc.handleDeletePod(cacheKey)
	case "Service":
		dc.handleDeleteService(cacheKey)
	}

	delete(dc.uidKindCache, cacheKey)
	delete(dc.dpCache, cacheKey)
	delete(dc.dimPropCache, cacheKey)
}

// HandleDelete accepts an object that has been deleted and removes the
// associated datapoints/props from the cache.  MUST HOLD LOCK!!
func (dc *DatapointCache) HandleDelete(oldObj runtime.Object) interface{} {
	key, err := keyForObject(oldObj)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
			"obj":   spew.Sdump(oldObj),
		}).Error("Could not get cache key")
		return nil
	}
	dc.DeleteByKey(key)
	return key
}

// HandleAdd accepts a new (or updated) object and updates the datapoint/prop
// cache as needed.  MUST HOLD LOCK!!
func (dc *DatapointCache) HandleAdd(newObj runtime.Object) interface{} {
	var dps []*datapoint.Datapoint
	var dimProps *atypes.DimProperties
	var kind string

	switch o := newObj.(type) {
	case *v1.Pod:
		dps, dimProps = dc.handleAddPod(o)
		kind = "Pod"
	case *v1.Namespace:
		dps = datapointsForNamespace(o)
		kind = "Namespace"
	case *v1.ReplicationController:
		dps = datapointsForReplicationController(o)
		kind = "ReplicationController"
	case *v1beta1.DaemonSet:
		dps = datapointsForDaemonSet(o)
		kind = "DaemonSet"
	case *v1beta1.Deployment:
		dps = datapointsForDeployment(o)
		dimProps = dimPropsForDeployment(o)
		kind = "Deployment"
	case *v1beta1.ReplicaSet:
		dps = datapointsForReplicaSet(o)
		dimProps = dimPropsForReplicaSet(o)
		kind = "ReplicaSet"
	case *v1.ResourceQuota:
		dps = datapointsForResourceQuota(o)
		kind = "ResourceQuota"
	case *v1.Node:
		dps = datapointsForNode(o, dc.useNodeName)
		dimProps = dimPropsForNode(o, dc.useNodeName)
		kind = "Node"
	case *v1.Service:
		dc.handleAddService(o)
		kind = "Service"
	default:
		log.WithFields(log.Fields{
			"obj": spew.Sdump(newObj),
		}).Error("Unknown object type in HandleAdd")
		return nil
	}

	key, err := keyForObject(newObj)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
			"obj":   spew.Sdump(newObj),
		}).Error("Could not get cache key")
		return nil
	}

	if dps != nil {
		dc.dpCache[key] = dps
	}
	if kind != "" {
		dc.uidKindCache[key] = kind
	}
	if dimProps != nil {
		dc.addDimPropsToCache(key, dimProps)
	}

	return key
}

type propertyLink struct {
	SourceProperty string
	SourceKind     string
	SourceJoinKey  string
	TargetProperty string
	TargetKind     string
	TargetJoinKey  string
}

// addDimPropsToCache maps and syncs properties from different resources together and adds
// them to the cache
func (dc *DatapointCache) addDimPropsToCache(key types.UID, dimProps *atypes.DimProperties) {
	links := []propertyLink{
		// TODO: disable linking until we figure out a more efficient way of
		// doing this.  This DOESN'T scale with 1000s of pods/resources.
		//propertyLink{
		//	SourceKind:     "ReplicaSet",
		//	SourceProperty: "deployment",
		//	SourceJoinKey:  "name",
		//	TargetKind:     "Pod",
		//	TargetProperty: "deployment",
		//	TargetJoinKey:  "replicaSet",
		//},
	}

	for _, link := range links {
		if dc.uidKindCache[key] == link.TargetKind {
			for cachedKey := range dc.dimPropCache {
				if dc.uidKindCache[cachedKey] == link.SourceKind {
					cachedProps := dc.dimPropCache[cachedKey].Properties
					if cachedProps[link.SourceJoinKey] != "" &&
						cachedProps[link.SourceJoinKey] == dimProps.Properties[link.TargetJoinKey] {
						dimProps.Properties[link.TargetProperty] = cachedProps[link.SourceProperty]
					}
				}
			}
		}
		if dc.uidKindCache[key] == link.SourceKind {
			for cachedKey := range dc.dimPropCache {
				if dc.uidKindCache[cachedKey] == link.TargetKind {
					cachedProps := dc.dimPropCache[cachedKey].Properties
					if cachedProps[link.TargetJoinKey] != "" &&
						cachedProps[link.TargetJoinKey] == dimProps.Properties[link.SourceJoinKey] {
						cachedProps[link.TargetProperty] = dimProps.Properties[link.SourceProperty]
					}
				}
			}
		}
	}

	dc.dimPropCache[key] = dimProps
}

// addPropertiesToDimProps adds/updates new properties to the DimProps cache
// given a cache key (types.UID) and new properties to add or update.
func (dc *DatapointCache) addPropertiesToDimProps(key interface{},
	newProps map[string]string) {

	cacheKey := key.(types.UID)
	for k, v := range newProps {
		dc.dimPropCache[cacheKey].Properties[k] = v
	}
}

// deletePropertiesFromDimProps attempts to delete a given list of properties
// for a resource, given a cache key (uid).
func (dc *DatapointCache) deletePropertiesFromDimProps(key interface{},
	propsToDelete []string) {

	cacheKey := key.(types.UID)
	for _, prop := range propsToDelete {
		delete(dc.dimPropCache[cacheKey].Properties, prop)
	}
}

// AllDatapoints returns all of the cached datapoints.
func (dc *DatapointCache) AllDatapoints() []*datapoint.Datapoint {
	dps := make([]*datapoint.Datapoint, 0)

	dc.Lock()
	defer dc.Unlock()

	for k := range dc.dpCache {
		if dc.dpCache[k] != nil {
			for i := range dc.dpCache[k] {
				// Copy the datapoint since nothing in datapoints is thread
				// safe.
				dp := *dc.dpCache[k][i]
				dps = append(dps, &dp)
			}
		}
	}

	return dps
}

// AllDimProperties returns any dimension properties pertaining to the cluster
func (dc *DatapointCache) AllDimProperties() []*atypes.DimProperties {
	dimProps := make([]*atypes.DimProperties, 0)

	dc.Lock()
	defer dc.Unlock()

	for k := range dc.dimPropCache {
		if dc.dimPropCache[k] != nil {
			clonedDimProps := dc.dimPropCache[k].Copy()
			dimProps = append(dimProps, clonedDimProps)
		}
	}

	return dimProps
}

// handleAddPod gets datapoints and dim props for a pod object, and adds
// the pod to the service:pod cache. If a service is matched, adds the
// service property to the pod.
func (dc *DatapointCache) handleAddPod(pod *v1.Pod) ([]*datapoint.Datapoint,
	*atypes.DimProperties) {
	dps := datapointsForPod(pod)
	dimProps := dimPropsForPod(pod)
	dc.podServiceCache.SetPod(pod)
	service, err := dc.podServiceCache.GetServiceNameForPod(pod)
	if err == nil {
		dimProps.Properties["service"] = service
	}
	return dps, dimProps
}

func (dc *DatapointCache) handleDeletePod(key interface{}) {
	cacheKey := key.(types.UID)
	dc.podServiceCache.DeletePodFromCache(cacheKey)
}

// handleAddService adds a service to the cache and adds the "service" property
// to each matching pod that the service selector matches
func (dc *DatapointCache) handleAddService(svc *v1.Service) {
	dc.podServiceCache.SetService(svc)
	podUIDs := dc.podServiceCache.GetPodUIDsForService(svc)
	dc.updateServicePropForPods(podUIDs)
}

// handleDeleteService removes a service from the cache. After removing
// the service from the cache, we need to update the "orphaned" pods
// that may now match another service, or no service.
func (dc *DatapointCache) handleDeleteService(key interface{}) {
	cacheKey := key.(types.UID)
	podUIDs := dc.podServiceCache.GetPodUIDsForServiceUID(cacheKey)
	dc.podServiceCache.DeleteServiceFromCache(cacheKey)
	dc.updateServicePropForPods(podUIDs)
}

// updateServicePropForPods takes a list of pod UIDs, gets the matching
// service for the pod, and adds the service property to the pod if one exists
func (dc *DatapointCache) updateServicePropForPods(podUIDs []types.UID) {

	for _, podUID := range podUIDs {
		service, err := dc.podServiceCache.GetServiceNameForPodUID(podUID)
		log.WithFields(log.Fields{
			"service": service,
			"err":     err,
			"pod":     podUID,
		}).Info("Adding/Removing service property to pod")
		if err != nil {
			delete(dc.dimPropCache[podUID].Properties, "service")
		} else {
			dc.dimPropCache[podUID].Properties["service"] = service
		}
	}
}
