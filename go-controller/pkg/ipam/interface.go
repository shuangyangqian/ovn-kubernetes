package ipam

import (
	"context"
)

const keyPrefix = "/ovn-k8s-controller/resource/ipam/"

type Resource interface {
	Key() string
	Value() ([]byte, error)
	Operator
}

type Operator interface {
	Create(ctx context.Context, c *EtcdV3Client) (string, string, error)
	Update(ctx context.Context, c *EtcdV3Client) (string, string, error)
	Delete(ctx context.Context, c *EtcdV3Client) error
	Get(ctx context.Context, c *EtcdV3Client) (map[string]string, error)
	//Watch(ctx context.Context, r *Resource)
}
