package register

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/etcd/clientv3"
	"github.com/hsyan2008/go-logger"
	"github.com/hsyan2008/hfw/grpc/discovery/resolver"
	"github.com/hsyan2008/hfw/signal"
)

// Prefix should start and end with no slash
var Prefix = "etcd3_naming"

type EtcdRegister struct {
	target []string
	ttl    int

	//存在etcd里的key
	key string
	//本服务的地址
	addr string

	client *clientv3.Client

	registerInfo RegisterInfo

	ctx context.Context
}

func NewEtcdRegister(target []string, ttl int) *EtcdRegister {
	return &EtcdRegister{target: target, ttl: ttl, ctx: signal.GetSignalContext().Ctx}
}

// Register register service with name as prefix to etcd, multi etcd addr should use ; to split
func (er *EtcdRegister) Register(info RegisterInfo) (err error) {
	if er.client == nil {
		er.client, err = clientv3.New(clientv3.Config{
			Endpoints:   er.target,
			DialTimeout: 5 * time.Second,
		})
		if err != nil {
			return err
		}
	}

	er.addr = fmt.Sprintf("%s:%d", info.Host, info.Port)
	er.key = fmt.Sprintf("/%s/%s/%s", resolver.EtcdResolver, info.ServiceName, er.addr)

	ticker := time.NewTicker(time.Second * time.Duration(info.UpdateInterval))

	go func() {
		for {
			getResp, err := er.client.Get(er.ctx, er.key)
			logger.Debug(getResp, err)
			if err != nil {
				logger.Warn(err)
			} else if getResp.Count == 0 {
				err = er.withAlive()
				if err != nil {
					logger.Warn(err)
				}
			} else {
				// do nothing
			}
			select {
			case <-signal.GetSignalContext().Ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	signal.GetSignalContext().WgAdd()

	return nil
}

func (er *EtcdRegister) withAlive() error {
	leaseResp, err := er.client.Grant(er.ctx, int64(er.ttl))
	if err != nil {
		return err
	}

	_, err = er.client.Put(er.ctx, er.key, er.addr, clientv3.WithLease(leaseResp.ID))
	if err != nil {
		return err
	}

	_, err = er.client.KeepAlive(er.ctx, leaseResp.ID)
	if err != nil {
		return err
	}
	return nil
}

// UnRegister remove service from etcd
func (er *EtcdRegister) UnRegister() (err error) {
	if er.client != nil {
		defer signal.GetSignalContext().WgDone()
		_, err = er.client.Delete(context.Background(), er.key)
	}

	return
}
