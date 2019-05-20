package cluster

import (
	"github.com/openvswitch/ovn-kubernetes/go-controller/pkg/values"
	kapi "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift/origin/pkg/util/netutils"
	"github.com/sirupsen/logrus"
)

// StartClusterMaster runs a subnet IPAM and a controller that watches arrival/departure
// of namespaces in the cluster
// On an addition to the cluster (namespace create), a new subnet is created for it that will translate
// to creation of a logical switch (done by the namespace, but could be created here at the master process too)
// Upon deletion of a namespace, the switch will be deleted
//
// TODO: Verify that the cluster was not already called with a different global subnet
//  If true, then either quit or perform a complete reconfiguration of the cluster (recreate switches/routers with new subnet values)
func (cluster *OvnClusterController) StartClusterMaster(masterNodeName string) error {

	masterSubnetAllocatorList := make([]*netutils.SubnetAllocator, 0)
	for _, clusterEntry := range cluster.ClusterIPNet {
		subrange := make([]string, 0)

		namespaces, err := cluster.Kube.GetNamespaces()
		if err != nil {
			return err
		}
		for _, item := range namespaces.Items {
			if item.Annotations[values.NamespaceSubnet] != "" {
				subrange = append(subrange, item.Annotations[values.NamespaceSubnet])
			}
		}
		subnetAllocator, err := netutils.NewSubnetAllocator(clusterEntry.CIDR.String(), 32-clusterEntry.HostSubnetLength, subrange)
		if err != nil {
			return err
		}
		masterSubnetAllocatorList = append(masterSubnetAllocatorList, subnetAllocator)
	}
	cluster.masterSubnetAllocatorList = masterSubnetAllocatorList

	return cluster.watchNamespaces()
}

func (cluster *OvnClusterController) watchNamespaces() error {
	_, err := cluster.watchFactory.AddNamespaceHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			namespace := obj.(*kapi.Namespace)
			logrus.Debugf("Added event for Namespace %q", namespace.Name)
			err := cluster.addNamespace(namespace)
			if err != nil {
				logrus.Errorf("error creating subnet for namespace %s: %v", namespace.Name, err)
			}
		},
		UpdateFunc: func(old, new interface{}) {},
		DeleteFunc: func(obj interface{}) {
			namespace := obj.(*kapi.Namespace)
			logrus.Debugf("Delete event for namespace %q", namespace.Name)
			namespaceSubnet, _ := parseNamespaceSubnet(namespace)
			err := cluster.deleteNamespace(namespace.Name, namespaceSubnet)
			if err != nil {
				logrus.Error(err)
			}
		},
	}, cluster.syncNamespaces)
	return err

}
