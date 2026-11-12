package fanout

import "testing"

func TestBufferDeliversToEverySubscriberAndDropsOneThatFallsBehind(t *testing.T) {
	buffer := New[int](2)
	fast, cancelFast := buffer.Subscribe()
	slow, _ := buffer.Subscribe()

	buffer.Publish(1)
	buffer.Publish(2)
	if got := <-fast; got != 1 {
		t.Fatalf("fast subscriber got %d, want 1", got)
	}
	buffer.Publish(3) // slow's queue of two is full: it is dropped

	if first, second := <-slow, <-slow; first != 1 || second != 2 {
		t.Fatalf("slow subscriber got %d, %d; want its two queued events", first, second)
	}
	if _, open := <-slow; open {
		t.Fatal("a subscriber that fell behind must be closed")
	}
	if second, third := <-fast, <-fast; second != 2 || third != 3 {
		t.Fatalf("fast subscriber got %d, %d; want 2, 3", second, third)
	}

	cancelFast()
	cancelFast()
	if _, open := <-fast; open {
		t.Fatal("a cancelled subscription must be closed")
	}
	buffer.Publish(4) // no subscribers left; must not block or panic
}
