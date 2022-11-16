package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/meshplus/bitxhub-kit/crypto"
	"github.com/meshplus/bitxhub-kit/crypto/asym"
	"github.com/meshplus/bitxhub-kit/log"
	"github.com/meshplus/bitxhub-kit/types"
	"github.com/meshplus/bitxhub-model/pb"
	"github.com/meshplus/go-bitxhub-client"
	"github.com/sirupsen/logrus"
)

func NewClient(pk crypto.PrivateKey, logger *logrus.Logger) (*rpcx.ChainClient, error) {
	addrs := []string{
		"172.16.13.130:60011",
		"172.16.13.130:60012",
		"172.16.13.130:60013",
		"172.16.13.130:60014",
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
		rpcx.WithPoolSize(256),
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

func handleShutdown(cancel context.CancelFunc) {
	var stop = make(chan os.Signal)
	signal.Notify(stop, syscall.SIGTERM)
	signal.Notify(stop, syscall.SIGINT)
	go func() {
		<-stop
		fmt.Println("received interrupt signal, shutting down...")
		cancel()
		os.Exit(0)
	}()
}

func main() {
	logger := log.New()
	var goroutines int
	var round int
	var poolSize int

	flag.IntVar(&goroutines, "goroutines", 200,
		"The number of concurrent go routines to get mutiSign from bitxhub")
	flag.IntVar(&round, "round", 100,
		"The round of concurrent go routines to get mutiSign from bitxhub")
	flag.IntVar(&poolSize, "size", 200, "The size in grpc client pool")
	flag.Parse()

	pk, _, err := keyPriv()
	if err != nil {
		fmt.Println(err)
		return
	}
	client, err := NewClient(pk, logger)
	if err != nil {
		fmt.Println(err)
		return
	}
	var (
		avCnt     = int64(0)
		avLatency = int64(0)
		queryCnt  = int64(0)
		latency   = int64(0)
	)
	totalTps := make([]int64, 0)
	ctx, cancel := context.WithCancel(context.Background())
	handleShutdown(cancel)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 1; i <= goroutines; i++ {
		go func(i int) {
			select {
			case <-ctx.Done():
				return
			default:
				defer wg.Done()
				for j := 0; j < round; j++ {
					now := time.Now()
					_, err = client.GetMultiSigns(fmt.Sprintf("%s%d", "1356:chainA:transfer-1356:chainB:transfer-", i), pb.GetSignsRequest_MULTI_IBTP_REQUEST)
					if err != nil {
						fmt.Println(err)
						continue
					}
					txLatency := time.Since(now).Milliseconds()
					atomic.AddInt64(&latency, txLatency)
					atomic.AddInt64(&avLatency, txLatency)
					atomic.AddInt64(&queryCnt, 1)
					atomic.AddInt64(&avCnt, 1)
				}
			}
		}(i)
	}

	tk := time.NewTicker(time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				avrageTps := avCnt
				avrageLatency := float64(avLatency) / float64(avCnt)
				totalTps = append(totalTps, avCnt)

				atomic.StoreInt64(&avCnt, 0)
				atomic.StoreInt64(&avLatency, 0)
				logger.Infof("%d %g ", avrageTps, avrageLatency)
			}
		}
	}()
	wg.Wait()
	cancel()

	skip := len(totalTps) / 8
	begin := skip
	end := len(totalTps) - skip
	total := int64(0)
	for _, v := range totalTps[begin:end] {
		total += v
	}
	logger.Infof("End Test, total query count is %d , average tps is %f , average latency is %fms",
		goroutines*round, float64(total)/float64(end-begin),
		float64(latency)/float64(queryCnt))
}
