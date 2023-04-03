package main

import (
	"context"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/meshplus/bitxhub-kit/crypto"
	"github.com/meshplus/bitxhub-kit/crypto/asym"
	"github.com/meshplus/bitxhub-kit/log"
	"github.com/meshplus/bitxhub-kit/types"
	"github.com/meshplus/go-bitxhub-client"
	"github.com/sirupsen/logrus"
)

var (
	goroutines int
	round      int
	poolSize   int
	mockPier   bool
	needRetry  bool
	signType   string
)

var interchain = "1356:eth1:0x056401B3E8e357e7C4ABDC0569a04447a01e1402-1356:eth2:0xb7bCC3F7f995A17c8C07B01deb66b2dFa0CFFDf5-1"

const (
	multiSign = "multiSign"
	tssSign   = "tssSign"
)

func NewClient(pk crypto.PrivateKey, logger *logrus.Logger, poolSize, initClientSize int) (*rpcx.ChainClient, error) {
	addrs := []string{
		"localhost:60011",
	}
	nodesInfo := make([]*rpcx.NodeInfo, 0, len(addrs))
	for _, addr := range addrs {
		nodeInfo := &rpcx.NodeInfo{Addr: addr}
		nodesInfo = append(nodesInfo, nodeInfo)
	}
	client, err := rpcx.New(
		rpcx.WithNodesInfo(nodesInfo...),
		rpcx.WithLogger(logger),
		rpcx.WithPrivateKey(pk),
		rpcx.WithPoolSize(poolSize),
		rpcx.WithInitClientSize(initClientSize),
	)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// keyPriv return privateKey and address
func keyPriv() (crypto.PrivateKey, *types.Address, error) {
	pk, err := asym.GenerateKeyPair(crypto.Secp256k1)
	if err != nil {
		return nil, nil, err
	}
	from, err := pk.PublicKey().Address()
	if err != nil {
		return nil, nil, err
	}
	return pk, from, nil
}

func Init() {
	flag.IntVar(&goroutines, "g", 200,
		"The number of concurrent go routines to get mutiSign from bitxhub")
	flag.IntVar(&round, "r", 100,
		"The round of concurrent go routines to get mutiSign from bitxhub")
	flag.IntVar(&poolSize, "size", 200, "The size in grpc client pool")
	flag.StringVar(&signType, "t", "multiSign", "The sign type")
	flag.BoolVar(&mockPier, "mock_pier", false, "mock pier to get multiSign")
	flag.BoolVar(&needRetry, "need_retry", false, "retry query for mock pier flag")
}

func main() {
	logger := log.New()
	Init()
	flag.Parse()
	logger.WithFields(logrus.Fields{"goroutines": goroutines, "round": round, "poolSize": poolSize,
		"mockPier": mockPier, "needRetry": needRetry}).Info("input param")

	pk, _, err := keyPriv()
	if err != nil {
		fmt.Println(err)
		return
	}
	client, err := NewClient(pk, logger, poolSize, poolSize)
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	req := &Request{
		client:   client,
		totalTps: make([]int64, 0),
		ctx:      ctx,
		cancel:   cancel,
		logger:   logger,
		batch:    make([]int, 0),
		batchCh:  make(chan []int, 1024),
	}
	req.handleShutdown(cancel)

	// listen average second tps
	tk := time.NewTicker(time.Second)
	go req.listen(tk)

	if mockPier {
		req.mockPierQuery()
	} else {
		wg := &sync.WaitGroup{}
		wg.Add(goroutines)
		req.getMultiSign(wg)
		wg.Wait()
		req.cancel()
	}

	skip := len(req.totalTps) / 8
	begin := skip
	end := len(req.totalTps) - skip
	total := int64(0)
	for _, v := range req.totalTps[begin:end] {
		total += v
	}
	logger.Infof("End Test, total query count is %d , average tps is %f , average latency is %fms",
		goroutines*round, float64(total)/float64(end-begin),
		float64(req.latency)/float64(req.queryCnt))
}
