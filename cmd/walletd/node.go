package main

import (
	"log"
	"net"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
	"go.sia.tech/core/chain"
	"go.sia.tech/core/gateway"
	"go.sia.tech/walletd/internal/syncerutil"
	"go.sia.tech/walletd/internal/walletutil"
	"go.sia.tech/walletd/syncer"
)

var mainnetBootstrap = []string{
	"108.227.62.195:9981",
	"139.162.81.190:9991",
	"144.217.7.188:9981",
	"147.182.196.252:9981",
	"15.235.85.30:9981",
	"167.235.234.84:9981",
	"173.235.144.230:9981",
	"198.98.53.144:7791",
	"199.27.255.169:9981",
	"2.136.192.200:9981",
	"213.159.50.43:9981",
	"24.253.116.61:9981",
	"46.249.226.103:9981",
	"5.165.236.113:9981",
	"5.252.226.131:9981",
	"54.38.120.222:9981",
	"62.210.136.25:9981",
	"63.135.62.123:9981",
	"65.21.93.245:9981",
	"75.165.149.114:9981",
	"77.51.200.125:9981",
	"81.6.58.121:9981",
	"83.194.193.156:9981",
	"84.39.246.63:9981",
	"87.99.166.34:9981",
	"91.214.242.11:9981",
	"93.105.88.181:9981",
	"93.180.191.86:9981",
	"94.130.220.162:9981",
}

var zenBootstrap = []string{
	"147.135.16.182:9881",
	"147.135.39.109:9881",
	"51.81.208.10:9881",
}

type boltDB struct {
	tx *bolt.Tx
	db *bolt.DB
}

func (db *boltDB) newTx() (err error) {
	if db.tx == nil {
		db.tx, err = db.db.Begin(true)
	}
	return
}

func (db *boltDB) Bucket(name []byte) chain.DBBucket {
	if err := db.newTx(); err != nil {
		panic(err)
	}

	b := db.tx.Bucket(name)
	if b == nil {
		return nil
	}
	return b
}

func (db *boltDB) CreateBucket(name []byte) (chain.DBBucket, error) {
	if err := db.newTx(); err != nil {
		return nil, err
	}

	b, err := db.tx.CreateBucket(name)
	if b == nil {
		return nil, err
	}
	return b, nil
}

func (db *boltDB) Flush() error {
	if db.tx == nil {
		return nil
	}

	if err := db.tx.Commit(); err != nil {
		return err
	}
	db.tx = nil
	return nil
}

func (db *boltDB) Cancel() {
	if db.tx == nil {
		return
	}

	db.tx.Rollback()
	db.tx = nil
}

type node struct {
	cm *chain.Manager
	s  *syncer.Syncer
	wm *walletutil.JSONWalletManager

	Start func() (stop func())
}

func newNode(addr, dir string, zen bool) (*node, error) {
	bdb, err := bolt.Open(filepath.Join(dir, "consensus.db"), 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
	network, genesisBlock := chain.Mainnet()
	if zen {
		network, genesisBlock = chain.TestnetZen()
	}
	dbstore, tip, err := chain.NewDBStore(&boltDB{db: bdb}, network, genesisBlock)
	if err != nil {
		return nil, err
	}
	cm := chain.NewManager(dbstore, tip.State)

	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	ps, err := syncerutil.NewJSONPeerStore(filepath.Join(dir, "peers.json"))
	if err != nil {
		log.Fatal(err)
	}
	bootstrapPeers := mainnetBootstrap
	if zen {
		bootstrapPeers = zenBootstrap
	}
	for _, peer := range bootstrapPeers {
		ps.AddPeer(peer)
	}
	header := gateway.Header{
		GenesisID:  genesisBlock.ID(),
		UniqueID:   gateway.GenerateUniqueID(),
		NetAddress: l.Addr().String(),
	}
	s := syncer.New(l, cm, ps, header, syncer.WithLogger(log.Default()))

	wm, err := walletutil.NewJSONWalletManager(dir, cm)
	if err != nil {
		return nil, err
	}

	return &node{
		cm: cm,
		s:  s,
		wm: wm,
		Start: func() func() {
			ch := make(chan struct{})
			go func() {
				s.Run()
				close(ch)
			}()
			return func() {
				l.Close()
				<-ch
				bdb.Close()
			}
		},
	}, nil
}
