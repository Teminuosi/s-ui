package service

import (
	"errors"
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
//
// 后端只看得见已经到达的请求,还在路上的看不见,所以客户端存完配置后应先查
// api/status?r=restart,等 pending 变 false(重启做完)再发后面的请求(网页前端就是这么做的)。

var (
	apiInflight int64
	apiLastDone int64 // UnixNano

	// 上一次启动 / 重启还没结束
	errCoreBusy = errors.New("core start in progress")

	coreRestarts = &restartQueue{
		idle:     panelIdle,
		tick:     50 * time.Millisecond,
		busyTick: 100 * time.Millisecond,
		maxWait:  restartMaxWait,
	}
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

// restartQueue:同一时间只有一个协程负责重启。
//   - 等待期间再存的配置合并进同一次重启(重启读的是库里最新的配置);
//   - 重启已经开始读配置后又存了新的,做完这次再重启一次,不丢;
//   - 别处的启动还没结束(远程 rule_set 下载慢时可能几十秒)就一直等它结束,不跳过。
type restartQueue struct {
	mu      sync.Mutex
	wanted  bool // 有新存的配置还没被哪次重启读到
	running bool // 负责重启的协程在跑(从排上到最后一次重启做完)

	idle     func() bool
	tick     time.Duration
	busyTick time.Duration
	maxWait  time.Duration
}

// request 排一次重启。run 返回 errCoreBusy 表示别的启动在进行、这次什么也没做。
func (q *restartQueue) request(run func() error) {
	q.mu.Lock()
	q.wanted = true
	if q.running {
		q.mu.Unlock()
		return
	}
	q.running = true
	q.mu.Unlock()
	go q.loop(run)
}

// pending:存过的配置还没全部应用到内核(在等空闲、等上一次启动,或正在重启)。
func (q *restartQueue) pending() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.running
}

func (q *restartQueue) loop(run func() error) {
	for {
		start := time.Now()
		for !q.idle() && time.Since(start) < q.maxWait {
			time.Sleep(q.tick)
		}
		for {
			// 先清标记再读配置:之后再存的配置会让下面再来一轮
			q.mu.Lock()
			q.wanted = false
			q.mu.Unlock()
			if !errors.Is(run(), errCoreBusy) {
				break
			}
			time.Sleep(q.busyTick)
		}
		q.mu.Lock()
		if !q.wanted {
			q.running = false
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()
	}
}

// RestartCoreWhenIdle 安排一次内核重启(用库里最新的配置)。必须在存配置的事务提交之后调用。
func (s *ConfigService) RestartCoreWhenIdle() {
	coreRestarts.request(func() error { return s.restartCoreWithConfig(nil) })
}

// CoreRestartPending:存过的配置还没全部应用到内核。
func CoreRestartPending() bool {
	return coreRestarts.pending()
}
