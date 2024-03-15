package tracker

import (
	"plane.watch/lib/tracker/mode_s"
	"sync"
)

type (
	lossyFrame struct {
		item       *mode_s.Frame
		next, prev *lossyFrame
	}
	lossyFrameList struct {
		head     *lossyFrame
		tail     *lossyFrame
		capacity int
		numItems int
		mu       sync.Mutex
	}
)

func newLossyFrameList(numItems int) lossyFrameList {
	return lossyFrameList{
		head:     nil,
		tail:     nil,
		capacity: numItems,
		numItems: 0,
	}
}

func (fl *lossyFrameList) Push(f *mode_s.Frame) {
	// defer is a stack, last in -> first out
	// called second to avoid lock contention
	defer func() {
		if fl.numItems > fl.capacity {
			_ = fl.Unshift()
		}
	}()

	fl.mu.Lock()
	defer fl.mu.Unlock()
	item := lossyFrame{
		item: f,
		next: fl.head,
		prev: nil,
	}
	if nil != fl.head {
		fl.head.prev = &item
	}
	fl.head = &item
	if nil == fl.tail {
		fl.tail = fl.head
	}
	fl.numItems++
}

// Unshift shortens the queue by 1, taking from the tail.
func (fl *lossyFrameList) Unshift() *mode_s.Frame {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	if nil == fl.tail {
		return nil
	}
	t := fl.tail
	fl.numItems--
	fl.tail = t.prev
	if nil != t.prev {
		fl.tail.next = nil
	}
	t.prev = nil
	t.next = nil
	if fl.numItems == 0 {
		fl.tail = nil
		fl.head = nil
	}

	defer func() {
		t = nil
	}()

	return t.item
}

func (fl *lossyFrameList) Pop() *mode_s.Frame {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	if nil == fl.head {
		return nil
	}
	t := fl.head
	fl.numItems--
	fl.head = t.next
	t.prev = nil
	t.next = nil
	if fl.numItems == 0 {
		fl.tail = nil
		fl.head = nil
	}

	defer func() {
		t = nil
	}()

	return t.item
}

func (fl *lossyFrameList) Len() int {
	return fl.numItems
}

func (fl *lossyFrameList) Range(f func(f *mode_s.Frame) bool) {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	t := fl.head
	for t != nil && t.next != nil {
		if !f(t.item) {
			return
		}
		t = t.next
	}
}
