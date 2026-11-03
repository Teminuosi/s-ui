package service

import (
	"sync"
	"sync/atomic"
	"time"
)

// 存配置之后重启内核:等这次的回复发出去、面板没有请求在处理时再重启。
//
// 浏览器(或 TMS 客户端)常常是经这台机器自己的节点打开面板的,请求就走在这个内核上。
// 以前存配置时立刻重启内核,回复还没送回去连接就被切断 —— 前端判成「写入失败」,
// 配置其实已经存上了;中转向导据此去撤出站,留下指向不存在出站的路由(真机 + Docker 复现)。
// 也不能只固定延迟一会儿:前端 / 客户端存完配置紧接着还会发请求(拉链接、测落地),
// 固定时刻重启会切断正好在途的那一个。所以等「没有在处理的请求、且空闲了一小会儿」再重启,
// 最多等 restartMaxWait,防止有长请求时永远不重启。

var (
	apiInflight    int64
	apiLastDone    int64 // UnixNano
	restartMu      sync.Mutex
	restartPending bool
)

const (
	restartIdle    = 300 * time.Millisecond
	restartMaxWait = 5 * time.Second
)

// TrackRequest 在面板的每个请求开始时调用,返回的函数在请求处理完时调用。
func TrackRequest() func() {
	atomic.AddInt64(&apiInflight, 1)
	return func() {
		atomic.StoreInt64(&apiLastDone, time.Now().UnixNano())
		atomic.AddInt64(&apiInflight, -1)
	}
}

func panelIdle() bool {
	return atomic.LoadInt64(&apiInflight) == 0 &&
		time.Since(time.Unix(0, atomic.LoadInt64(&apiLastDone))) >= restartIdle
}

func startCoreBusy() bool {
	startCoreMu.Lock()
	defer startCoreMu.Unlock()
	return startCoreInProgress
}

// RestartCoreWhenIdle 安排一次内核重启(用库里最新的配置)。等待期间再存的配置合并进同一次重启。
func (s *ConfigService) RestartCoreWhenIdle() {
	restartMu.Lock()
	if restartPending {
		restartMu.Unlock()
		return
	}
	restartPending = true
	restartMu.Unlock()

	go func() {
		start := time.Now()
		for time.Since(start) < restartMaxWait {
			time.Sleep(50 * time.Millisecond)
			if panelIdle() {
				break
			}
		}
		// 先清标记再读配置:之后再存的配置会另排一次重启,不会漏
		restartMu.Lock()
		restartPending = false
		restartMu.Unlock()
		// 上一次启动还没完成时 restartCoreWithConfig 会直接跳过,等它完成
		for i := 0; i < 50 && startCoreBusy(); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		_ = s.restartCoreWithConfig(nil)
	}()
}
