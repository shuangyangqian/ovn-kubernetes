package ipam

import (
	"context"
	"encoding/json"
	"fmt"
	"go.etcd.io/etcd/clientv3"
	"net"
)

const PodEndpointResourceType = "pod-workload-endpoint"
const podPrefix = keyPrefix + "/" + PodEndpointResourceType

type PodWorkloadEndpoint struct {
	// pod name
	Pod       string
	Namespace string
	IP        net.IP
	MASK      int
	MAC       net.HardwareAddr
	// pause container ID in pod
	ContainerID string
	Annotations map[string]string
}

func (pwed *PodWorkloadEndpoint) Key() string {
	podSuffix := pwed.Namespace + "_" + pwed.Pod
	return fmt.Sprintf("%s/%s", podPrefix, podSuffix)
}

func (pwed *PodWorkloadEndpoint) Value() ([]byte, error) {
	b, err := json.Marshal(pwed)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (pwed *PodWorkloadEndpoint) KeyAndValue() (string, string, error) {
	valueB, err := pwed.Value()
	if err != nil {
		return "", "", err
	}
	return pwed.Key(), string(valueB[:]), err
}

func (pwed *PodWorkloadEndpoint) Create(ctx context.Context, c *EtcdV3Client) (string, string, error) {
	key, value, err := pwed.KeyAndValue()
	if err != nil {
		return "", "", err
	}
	opOpts := []clientv3.OpOption{}
	return c.Create(ctx, key, value, opOpts)
}

func (pwed *PodWorkloadEndpoint) Update(ctx context.Context, c *EtcdV3Client) (string, string, error) {
	key, value, err := pwed.KeyAndValue()
	if err != nil {
		return "", "", err
	}

	opOpts := []clientv3.OpOption{}
	return c.Update(ctx, key, value, opOpts)

}

func (pwed *PodWorkloadEndpoint) Delete(ctx context.Context, c *EtcdV3Client) error {
	key := pwed.Key()
	return c.Delete(ctx, key)
}

func (pwed *PodWorkloadEndpoint) Get(ctx context.Context, c *EtcdV3Client) (map[string]string, error) {
	key, _, err := pwed.KeyAndValue()
	if err != nil {
		return nil, err
	}

	opOpts := []clientv3.OpOption{}
	return c.Get(ctx, key, opOpts)
}
