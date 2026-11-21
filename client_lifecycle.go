package main

import (
	"errors"
	"log"

	"github.com/xAmarnath/gogram/telegram"
)

// The application supplies ClientConfig.Cache, so it also owns closing it.
// Every failed initialization disposes the client/cache while preserving disk
// sessions; a network failure is not evidence that stored credentials are bad.
func openTelegramClient(conf telegram.ClientConfig, create func(telegram.ClientConfig) (*telegram.Client, error), initialize func(*telegram.Client) error) (_ *telegram.Client, err error) {
	var client *telegram.Client
	defer func() {
		if err == nil {
			return
		}
		if client != nil {
			err = errors.Join(err, closeTelegramClient(client))
		}
		if conf.Cache != nil && (client == nil || client.Cache != conf.Cache) {
			err = errors.Join(err, conf.Cache.Close())
		}
	}()
	client, err = create(conf)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("客户端初始化返回空对象")
	}
	if err = initialize(client); err != nil {
		return nil, err
	}
	return client, nil
}

// Terminate is for discarded clients, unlike transport-only Reconnect. Close the
// borrowed cache after stopping background work and before deleting any files.
func closeTelegramClient(client *telegram.Client) error {
	if client == nil {
		return nil
	}
	var err error
	if client.MTProto != nil {
		err = client.Terminate()
	}
	if client.Cache != nil {
		err = errors.Join(err, client.Cache.Close())
	}
	return err
}

func (infos *Infos) stopUserClient(discardSession bool) {
	infos.UserClientMu.Lock()
	defer infos.UserClientMu.Unlock()
	client := infos.UserClient.Swap(nil)
	infos.Status.Store(0)
	select {
	case <-infos.Code:
	default:
	}
	select {
	case <-infos.Pass:
	default:
	}
	if err := closeTelegramClient(client); err != nil {
		log.Printf("关闭 UserBot 资源失败: %+v", err)
	}
	if discardSession {
		cleanFiles(CleanRealm{Cate: "user", Realm: "session"})
		cleanFiles(CleanRealm{Cate: "user", Realm: "cache", Filter: false})
	}
}

// ensureUserClient publishes one fully initialized client, retaining any live
// instance instead of replacing it when startup and login commands overlap.
func (infos *Infos) ensureUserClient(create func() (*telegram.Client, error)) error {
	infos.UserClientMu.Lock()
	defer infos.UserClientMu.Unlock()
	if infos.UserClient.Load() != nil {
		return nil
	}
	client, err := create()
	if err != nil {
		return err
	}
	if client == nil {
		return errors.New("客户端初始化返回空对象")
	}
	infos.UserClient.Store(client)
	return nil
}

// A failed creator owns no published client. It must not close a client that a
// different waiting startup/login request may have published in the meantime.
func (infos *Infos) failedUserClientCreation() {
	infos.UserClientMu.Lock()
	defer infos.UserClientMu.Unlock()
	if infos.UserClient.Load() == nil {
		infos.Status.Store(0)
	}
}
