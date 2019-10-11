package broker

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockPeerWriter struct {
	mock.Mock
}

func (m *mockPeerWriter) BufferedAmount() uint64 {
	args := m.Called()
	return args.Get(0).(uint64)
}

func (m *mockPeerWriter) Write(p []byte) error {
	args := m.Called(p)
	return args.Error(0)
}

func TestFixedQueueWriterController(t *testing.T) {
	t.Run("if there is space, just send the data", func(t *testing.T) {
		w := &mockPeerWriter{}
		c := NewFixedQueueWriterController(w, 5, 10)

		msg := make([]byte, 1)
		w.On("BufferedAmount").Return(uint64(0))
		w.On("Write", msg).Return(nil).Twice()

		c.Write(msg)
		c.Write(msg)

		w.AssertExpectations(t)
	})

	t.Run("if there is no space, store it in the queue", func(t *testing.T) {
		w := &mockPeerWriter{}
		c := NewFixedQueueWriterController(w, 5, 10)

		w.On("BufferedAmount").Return(uint64(100)).Times(5)

		c.Write([]byte{0x01})
		c.Write([]byte{0x02})
		c.Write([]byte{0x03})
		c.Write([]byte{0x04})
		c.Write([]byte{0x05})

		require.Equal(t, 5, c.size)
		w.AssertExpectations(t)

		w.On("BufferedAmount").Return(uint64(0)).Times(5).
			On("Write", []byte{0x01}).Return(nil).Once().
			On("Write", []byte{0x02}).Return(nil).Once().
			On("Write", []byte{0x03}).Return(nil).Once().
			On("Write", []byte{0x04}).Return(nil).Once().
			On("Write", []byte{0x05}).Return(nil).Once()
		c.OnBufferedAmountLow()
		require.Equal(t, 0, c.size)
		require.Equal(t, uint32(0), c.GetDiscardedCount())
		w.AssertExpectations(t)

		// NOTE: expecting to do nothing
		c.OnBufferedAmountLow()
		require.Equal(t, 0, c.size)
		w.AssertExpectations(t)
	})

	t.Run("if there is no space, store a fixed amount in the queue", func(t *testing.T) {
		w := &mockPeerWriter{}
		c := NewFixedQueueWriterController(w, 5, 10)

		w.On("BufferedAmount").Return(uint64(100)).Times(6)

		c.Write([]byte{0x01})
		c.Write([]byte{0x02})
		c.Write([]byte{0x03})
		c.Write([]byte{0x04})
		c.Write([]byte{0x05})
		c.Write([]byte{0x06})

		require.Equal(t, 5, c.size)
		w.AssertExpectations(t)

		w.On("BufferedAmount").Return(uint64(0)).Times(5).
			On("Write", []byte{0x06}).Return(nil).Once().
			On("Write", []byte{0x02}).Return(nil).Once().
			On("Write", []byte{0x03}).Return(nil).Once().
			On("Write", []byte{0x04}).Return(nil).Once().
			On("Write", []byte{0x05}).Return(nil).Once()
		c.OnBufferedAmountLow()
		require.Equal(t, 0, c.size)
		require.Equal(t, uint32(1), c.GetDiscardedCount())
		w.AssertExpectations(t)

		// NOTE: expecting to do nothing
		c.OnBufferedAmountLow()
		require.Equal(t, 0, c.size)
		w.AssertExpectations(t)
	})
}
