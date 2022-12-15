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

	"github.com/Rican7/retry"
	"github.com/Rican7/retry/strategy"
	"github.com/meshplus/bitxhub-kit/crypto"
	"github.com/meshplus/bitxhub-kit/crypto/asym"
	"github.com/meshplus/bitxhub-kit/log"
	"github.com/meshplus/bitxhub-kit/types"
	"github.com/meshplus/bitxhub-model/pb"
	"github.com/meshplus/go-bitxhub-client"
	"github.com/sirupsen/logrus"
)

var goroutines int
var round int
var poolSize int
var mockPier bool
var needRetry bool
var signType string

const (
	MultiSign = "multiSign"
)

func NewClient(pk crypto.PrivateKey, logger *logrus.Logger, poolSize, initClientSize int) (*rpcx.ChainClient, error) {
	addrs := []string{
		"172.16.13.130:61011",
		"172.16.13.131:61012",
		"172.16.13.132:61013",
		"172.16.13.133:61014",
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

type request struct {
	client     rpcx.Client
	batchCh    chan []int
	batch      []int
	avCnt      int64
	avLatency  int64
	queryCnt   int64
	latency    int64
	maxLatency int64
	totalTps   []int64
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *logrus.Logger
}

func (req *request) handleShutdown(cancel context.CancelFunc) {
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
	req := &request{
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

func (req *request) mockPierQuery() {
	height := 1
	go func() {
		for {
			select {
			case batch := <-req.batchCh:
				var wg sync.WaitGroup
				wg.Add(len(batch))
				now := time.Now()
				for _, i := range batch {
					go func(i int) {
						defer wg.Done()
						current := time.Now()
						if err := retry.Retry(func(attempt uint) error {
							_, err := req.client.GetMultiSigns(fmt.Sprintf("%s%d", "1356:chainA:transfer-1356:chainB:transfer-", i), pb.GetSignsRequest_MULTI_IBTP_REQUEST)
							if err != nil {
								req.logger.Errorf("get sign err:%s", err)
								return err
							}
							return nil
						}, strategy.Wait(1*time.Second)); err != nil {
							req.logger.Panicf("get sign err:%s", err)
						}
						req.handleCnt(current)
					}(i)
					//req.logger.WithFields(logrus.Fields{"time": time.Since(now), "num": n}).Info("end handle batch")
				}
				wg.Wait()
				req.logger.WithFields(logrus.Fields{"time": time.Since(now), "height": height, "size": len(batch)}).Info("end handle batch")
				height++
			case <-req.ctx.Done():
				return
			}
		}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	index := 1
	size := 100
	for {
		select {
		case <-req.ctx.Done():
			return
		case <-ticker.C:
			fmt.Println(goroutines)
			fmt.Println(round)
			if index+size >= goroutines*round {
				size = goroutines*round - index + 1
				batch := make([]int, 0, size)
				for i := index; i < index+size; i++ {
					batch = append(batch, i)
				}
				req.batchCh <- batch
				if !needRetry {
					time.Sleep(1 * time.Second)
					req.cancel()
					return
				}
				index = 1
			}
			batch := make([]int, 0, size)
			for i := index; i < index+size; i++ {
				batch = append(batch, i)
			}
			req.batchCh <- batch
			index += size
		}
	}
}

func (req *request) listen(tk *time.Ticker) {
	for {
		select {
		case <-req.ctx.Done():
			return
		case <-tk.C:
			averageTps := req.avCnt
			averageLatency := float64(req.avLatency) / float64(req.avCnt)
			req.totalTps = append(req.totalTps, req.avCnt)

			atomic.StoreInt64(&req.avCnt, 0)
			atomic.StoreInt64(&req.avLatency, 0)
			req.logger.Infof("%d %g ", averageTps, averageLatency)
		}
	}
}

func (req *request) getMultiSign(wg *sync.WaitGroup) {
	for i := 1; i <= goroutines; i++ {
		go func(i int) {
			select {
			case <-req.ctx.Done():
				return
			default:
				defer wg.Done()
				for j := 0; j < round; j++ {
					var err error
					now := time.Now()
					if signType == MultiSign {
						_, err = req.client.GetMultiSigns(fmt.Sprintf("%s%d", "1356:chainA:transfer-1356:chainB:transfer-", i), pb.GetSignsRequest_MULTI_IBTP_REQUEST)
					} else {
						_, err = req.client.GetTssSigns(fmt.Sprintf("%s%d", "1356:chainA:transfer-1356:chainB:transfer-", i), pb.GetSignsRequest_TSS_IBTP_REQUEST, nil)
					}
					if err != nil {
						fmt.Println(err)
						continue
					}
					req.handleCnt(now)
				}
			}
		}(i)
	}
}

func (req *request) handleCnt(now time.Time) {
	txLatency := time.Since(now).Milliseconds()
	for {
		currentMax := atomic.LoadInt64(&req.maxLatency)
		if txLatency > currentMax {
			if atomic.CompareAndSwapInt64(&req.maxLatency, currentMax, txLatency) {
				break
			} else {
				continue
			}
		} else {
			break
		}
	}

	atomic.AddInt64(&req.latency, txLatency)
	atomic.AddInt64(&req.avLatency, txLatency)
	atomic.AddInt64(&req.queryCnt, 1)
	atomic.AddInt64(&req.avCnt, 1)
}
