package ipam

import (
	"context"
	"fmt"
	"github.com/openvswitch/ovn-kubernetes/go-controller/pkg/config"
	"github.com/openvswitch/ovn-kubernetes/go-controller/pkg/errors"
	log "github.com/sirupsen/logrus"
	"go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/pkg/transport"
	"strings"
	"time"
)

var (
	clientTimeout    = 10 * time.Second
	keepaliveTime    = 30 * time.Second
	keepaliveTimeout = 10 * time.Second
)

type EtcdV3Client struct {
	etcdClient *clientv3.Client
}

func NewEtcdV3Client(config *config.EtcdConfig) (*EtcdV3Client, error) {
	// Split the endpoints into a location slice.
	etcdLocation := []string{}
	if config.Endpoints != "" {
		etcdLocation = strings.Split(config.Endpoints, ",")
	}

	if len(etcdLocation) == 0 {
		message := "No etcd endpoints specified in etcdv3 API config"
		log.Warning(message)
		err := errors.ErrorWithMessage{
			Name: message,
		}
		return nil, err
	}

	// Create the etcd client
	tlsInfo := &transport.TLSInfo{
		TrustedCAFile: config.Cafile,
		CertFile:      config.CertFile,
		KeyFile:       config.KeyFile,
	}

	tls, err := tlsInfo.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("could not initialize etcdv3 client: %+v", err)
	}

	// Build the etcdv3 config.
	cfg := clientv3.Config{
		Endpoints:            etcdLocation,
		TLS:                  tls,
		DialTimeout:          clientTimeout,
		DialKeepAliveTime:    keepaliveTime,
		DialKeepAliveTimeout: keepaliveTimeout,
	}

	client, err := clientv3.New(cfg)
	if err != nil {
		return nil, err
	}

	return &EtcdV3Client{etcdClient: client}, nil
}

// Create an entry in the datastore.  If the entry already exists, this will return
// an ErrorResourceAlreadyExists error and the current entry.
func (c *EtcdV3Client) Create(ctx context.Context, key, value string,
	putOpts []clientv3.OpOption) (string, string, error) {

	// Checking for 0 version of the etcdKey, which means it doesn't exists yet,
	// and if it does, get the current value.
	log.Debug("Performing etcdv3 transaction for Create request")
	txnResp, err := c.etcdClient.Txn(ctx).If(
		clientv3.Compare(clientv3.Version(key), "=", 0),
	).Then(
		clientv3.OpPut(key, value, putOpts...),
	).Else(
		clientv3.OpGet(key),
	).Commit()
	if err != nil {
		log.WithError(err).Warning("Create failed")
		return "", "", errors.ErrorDatastoreError{Err: err}
	}

	if !txnResp.Succeeded {
		// The resource must already exist.  Extract the current newValue and
		// return that if possible.
		log.Info("Create transaction failed due to resource already existing")
		getResp := (*clientv3.GetResponse)(txnResp.Responses[0].GetResponseRange())
		if len(getResp.Kvs) != 0 {
			return string(getResp.Kvs[0].Key), string(getResp.Kvs[0].Value), nil
		}
	}

	return key, value, nil
}

// Update an entry in the datastore.  If the entry does not exist, this will return
// an ErrorResourceDoesNotExist error.  The ResourceVersion must be specified, and if
// incorrect will return a ErrorResourceUpdateConflict error and the current entry.
func (c *EtcdV3Client) Update(ctx context.Context, key, value string,
	opOpts []clientv3.OpOption) (string, string, error) {
	log.Debug("Processing Update request")

	conds := []clientv3.Cmp{clientv3.Compare(clientv3.Version(key), "!=", 0)}

	log.Debug("Performing etcdv3 transaction for Update request")
	txnResp, err := c.etcdClient.Txn(ctx).If(
		conds...,
	).Then(
		clientv3.OpPut(key, value, opOpts...),
	).Else(
		clientv3.OpGet(key),
	).Commit()

	if err != nil {
		log.WithError(err).Warning("Update failed")
		return "", "", errors.ErrorDatastoreError{Err: err}
	}

	// Etcd V3 does not return a error when compare condition fails we must verify the
	// response Succeeded field instead.  If the compare did not succeed then check for
	// a successful get to return either an UpdateConflict or a ResourceDoesNotExist error.
	if !txnResp.Succeeded {
		getResp := (*clientv3.GetResponse)(txnResp.Responses[0].GetResponseRange())
		if len(getResp.Kvs) == 0 {
			log.Info("Update transaction failed due to resource not existing")
			return "", "", errors.ErrorResourceDoesNotExist{Key: key}
		}

		log.Info("Update transaction failed due to resource update conflict")
		return string(getResp.Kvs[0].Key), string(getResp.Kvs[0].Value),
			errors.ErrorResourceUpdateConflict{Key: key}
	}

	return key, value, nil
}

// Delete an entry in the datastore.  This errors if the entry does not exists.
func (c *EtcdV3Client) Delete(ctx context.Context, key string) error {
	log.Debug("Processing Delete request")

	conds := []clientv3.Cmp{}

	log.Debug("Performing etcdv3 transaction for Delete request")
	txnResp, err := c.etcdClient.Txn(ctx).If(
		conds...,
	).Then(
		clientv3.OpDelete(key, clientv3.WithPrevKV()),
	).Else(
		clientv3.OpGet(key),
	).Commit()
	if err != nil {
		log.WithError(err).Warning("Delete failed")
		return errors.ErrorDatastoreError{Err: err}
	}

	// Transaction did not succeed - which means the ModifiedIndex check failed.  We can respond
	// with the latest settings.
	if !txnResp.Succeeded {
		log.Info("Delete transaction failed due to resource update conflict")

		getResp := txnResp.Responses[0].GetResponseRange()
		if len(getResp.Kvs) == 0 {
			log.Info("Delete transaction failed due resource not existing")
			return errors.ErrorResourceDoesNotExist{Key: key}
		}
		return errors.ErrorResourceUpdateConflict{Key: key}
	}

	return nil
}

// Get an entry from the datastore.  This errors if the entry does not exist.
func (c *EtcdV3Client) Get(ctx context.Context, key string,
	opOpts []clientv3.OpOption) (map[string]string, error) {
	log.Debug("Processing Get request")

	log.Debug("Calling Get on etcdv3 client")
	resp, err := c.etcdClient.Get(ctx, key, opOpts...)
	if err != nil {
		log.WithError(err).Info("Error returned from etcdv3 client")
		return nil, errors.ErrorDatastoreError{Err: err}
	}
	if len(resp.Kvs) == 0 {
		log.Debug("No results returned from etcdv3 client")
		return nil, errors.ErrorResourceDoesNotExist{Key: key}
	}

	m := make(map[string]string)
	for _, k := range resp.Kvs {
		m[string(k.Key)] = string(k.Value)
	}
	return m, nil
}
