package broker

import (
	"sync"
	"sync/atomic"
)

// WriterControllerFactory ...
type WriterControllerFactory = func(uint64, PeerWriter) WriterController

// UnboundedWriterController simply discard any packages when the BufferedAmount > maxBufferSize
type UnboundedWriterController struct {
	writer PeerWriter
}

// NewUnboundedWriterController creates a new UnboundedWriterController
func NewUnboundedWriterController(writer PeerWriter) *UnboundedWriterController {
	return &UnboundedWriterController{writer: writer}
}

// OnBufferedAmountLow ...
func (c *UnboundedWriterController) OnBufferedAmountLow() {}

// Write ...
func (c *UnboundedWriterController) Write(p []byte) {
	c.writer.Write(p) //nolint:gosec,errcheck
}

// DiscardWriterController simply discard any packages when the BufferedAmount > maxBufferSize
type DiscardWriterController struct {
	maxBufferSize  uint64
	writer         PeerWriter
	discardedCount uint32
}

// NewDiscardWriterController creates a new DiscardWriterController
func NewDiscardWriterController(writer PeerWriter, maxBufferSize uint64) *DiscardWriterController {
	return &DiscardWriterController{
		writer:        writer,
		maxBufferSize: maxBufferSize,
	}
}

// GetDiscardedCount ...
func (c *DiscardWriterController) GetDiscardedCount() uint32 {
	return atomic.LoadUint32(&c.discardedCount)
}

// OnBufferedAmountLow ...
func (c *DiscardWriterController) OnBufferedAmountLow() {}

// Write ...
func (c *DiscardWriterController) Write(p []byte) {
	if c.writer.BufferedAmount()+uint64(len(p)) < c.maxBufferSize {
		c.writer.Write(p) //nolint:gosec,errcheck
	} else {
		atomic.AddUint32(&c.discardedCount, 1)
	}
}

// BufferedWriterController provides an ever growing queue between the writer and the user
type BufferedWriterController struct {
	mux           sync.Mutex
	writer        PeerWriter
	maxBufferSize uint64
	queue         [][]byte
}

// NewBufferedWriterController creates a new BufferedWriterController
func NewBufferedWriterController(
	writer PeerWriter, initialQueueSize int, maxBufferSize uint64) *BufferedWriterController {
	return &BufferedWriterController{
		writer:        writer,
		maxBufferSize: maxBufferSize,
		queue:         make([][]byte, 0, initialQueueSize),
	}
}

// OnBufferedAmountLow ...
func (c *BufferedWriterController) OnBufferedAmountLow() {
	c.mux.Lock()
	defer c.mux.Unlock()

	for {
		if len(c.queue) == 0 {
			return
		}

		msg := c.queue[0]
		if c.writer.BufferedAmount()+uint64(len(msg)) < c.maxBufferSize {
			if err := c.writer.Write(msg); err != nil {
				return
			}

			c.queue = c.queue[1:]
		} else {
			break
		}
	}
}

// Write ...
func (c *BufferedWriterController) Write(p []byte) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if c.writer.BufferedAmount()+uint64(len(p)) < c.maxBufferSize {
		c.writer.Write(p) //nolint:gosec,errcheck
	} else {
		c.queue = append(c.queue, p)
	}
}

// FixedQueueWriterController simply discard any packages when the BufferedAmount > maxBufferSize
type FixedQueueWriterController struct {
	mux sync.Mutex

	maxBufferSize uint64
	writer        PeerWriter

	queue      [][]byte
	size       int
	head, tail int

	discardedCount uint32
}

// NewFixedQueueWriterController creates a new FixedQueueWriterController
func NewFixedQueueWriterController(writer PeerWriter, queueSize int, maxBufferSize uint64) *FixedQueueWriterController {
	return &FixedQueueWriterController{
		writer:        writer,
		maxBufferSize: maxBufferSize,
		queue:         make([][]byte, queueSize),
	}
}

// GetDiscardedCount ...
func (c *FixedQueueWriterController) GetDiscardedCount() uint32 {
	return atomic.LoadUint32(&c.discardedCount)
}

// OnBufferedAmountLow ...
func (c *FixedQueueWriterController) OnBufferedAmountLow() {
	for {
		c.mux.Lock()

		exit := true

		if c.size > 0 {
			msg := c.queue[c.tail]

			if c.writer.BufferedAmount()+uint64(len(msg)) < c.maxBufferSize {
				exit = false

				c.tail++

				if c.tail == len(c.queue) {
					c.tail = 0
				}

				c.size--

				if err := c.writer.Write(msg); err != nil {
					return
				}
			}
		}

		c.mux.Unlock()

		if exit {
			break
		}
	}
}

// Write ...
func (c *FixedQueueWriterController) Write(p []byte) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if c.writer.BufferedAmount()+uint64(len(p)) < c.maxBufferSize {
		c.writer.Write(p) //nolint:gosec,errcheck
	} else {
		c.queue[c.head] = p
		c.head++

		if c.head == len(c.queue) {
			c.head = 0
		}

		if c.size == len(c.queue) {
			atomic.AddUint32(&c.discardedCount, 1)
		} else {
			c.size++
		}
	}
}
