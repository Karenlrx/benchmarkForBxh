package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Rican7/retry"
	"github.com/Rican7/retry/strategy"
	"github.com/meshplus/bitxhub-model/pb"
	rpcx "github.com/meshplus/go-bitxhub-client"
	"github.com/sirupsen/logrus"
)

type Request struct {
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

func (req *Request) mockPierQuery() {
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
							_, err := req.client.GetMultiSigns(interchain,
								pb.GetSignsRequest_MULTI_IBTP_REQUEST)
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

func (req *Request) listen(tk *time.Ticker) {
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

func (req *Request) getMultiSign(wg *sync.WaitGroup) {
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
					switch signType {
					case multiSign:
						_, err = req.client.GetMultiSigns(interchain,
							pb.GetSignsRequest_MULTI_IBTP_REQUEST)
					case tssSign:
						_, err = req.client.GetTssSigns(interchain,
							pb.GetSignsRequest_TSS_IBTP_REQUEST, nil)
					default:
						req.logger.Errorf("sign type error: not support this sign type:%s", signType)
						return
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

func (req *Request) handleCnt(now time.Time) {
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

func (req *Request) handleShutdown(cancel context.CancelFunc) {
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
