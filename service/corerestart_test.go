package service

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestQueue(idle func() bool) *restartQueue {
	return &restartQueue{idle: idle, tick: time.Millisecond, busyTick: time.Millisecond, maxWait: 200 * time.Millisecond}
}

func waitDone(t *testing.T, q *restartQueue) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		if !q.pending() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("restart still pending after 2s")
}

// 等空闲期间多次存配置,合并成一次重启
func TestRestartQueueMerges(t *testing.T) {
	var idle atomic.Bool
	q := newTestQueue(idle.Load)
	var runs int32
	run := func() error { atomic.AddInt32(&runs, 1); return nil }
	q.request(run)
	q.request(run)
	q.request(run)
	if !q.pending() {
		t.Fatal("should be pending right after request")
	}
	time.Sleep(20 * time.Millisecond)
	if atomic.LoadInt32(&runs) != 0 {
		t.Fatal("restarted while panel busy")
	}
	idle.Store(true)
	waitDone(t, q)
	if n := atomic.LoadInt32(&runs); n != 1 {
		t.Fatalf("runs = %d, want 1", n)
	}
}

// 面板一直不空闲,最多等 maxWait 也要重启
func TestRestartQueueMaxWait(t *testing.T) {
	q := newTestQueue(func() bool { return false })
	var runs int32
	start := time.Now()
	q.request(func() error { atomic.AddInt32(&runs, 1); return nil })
	waitDone(t, q)
	if atomic.LoadInt32(&runs) != 1 || time.Since(start) < q.maxWait {
		t.Fatalf("runs=%d elapsed=%v", runs, time.Since(start))
	}
}

// 别的启动还在进行(远程 rule_set 慢):不跳过,等它结束再重启(应修 2b)
func TestRestartQueueWaitsForBusyStart(t *testing.T) {
	q := newTestQueue(func() bool { return true })
	var busy atomic.Bool
	busy.Store(true)
	var runs int32
	q.request(func() error {
		if busy.Load() {
			return errCoreBusy
		}
		atomic.AddInt32(&runs, 1)
		return nil
	})
	time.Sleep(50 * time.Millisecond)
	if !q.pending() || atomic.LoadInt32(&runs) != 0 {
		t.Fatal("should keep waiting while another start is in progress")
	}
	busy.Store(false)
	waitDone(t, q)
	if atomic.LoadInt32(&runs) != 1 {
		t.Fatalf("runs = %d, want 1", runs)
	}
}

// 重启已经读完配置、还在启动时又存了新的:做完这次再重启一次(应修 2b / 2a)
func TestRestartQueueReappliesSaveDuringRestart(t *testing.T) {
	q := newTestQueue(func() bool { return true })
	var mu sync.Mutex
	db, applied := "A", []string{}
	inRun := make(chan struct{})
	release := make(chan struct{})
	first := true
	run := func() error {
		mu.Lock()
		cfg := db // 读库
		mu.Unlock()
		if first {
			first = false
			close(inRun)
			<-release // 启动很慢
		}
		mu.Lock()
		applied = append(applied, cfg)
		mu.Unlock()
		return nil
	}
	q.request(run)
	<-inRun
	mu.Lock()
	db = "B" // 提交
	mu.Unlock()
	q.request(run) // 提交之后排
	close(release)
	waitDone(t, q)
	mu.Lock()
	defer mu.Unlock()
	if len(applied) == 0 || applied[len(applied)-1] != "B" {
		t.Fatalf("last applied config = %v, want B", applied)
	}
}

// 一轮做完后再存,重新排一轮
func TestRestartQueueRestartsAgainLater(t *testing.T) {
	q := newTestQueue(func() bool { return true })
	var runs int32
	run := func() error { atomic.AddInt32(&runs, 1); return nil }
	q.request(run)
	waitDone(t, q)
	q.request(run)
	waitDone(t, q)
	if atomic.LoadInt32(&runs) != 2 {
		t.Fatalf("runs = %d, want 2", runs)
	}
}
