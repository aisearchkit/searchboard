//! Bounded drop-oldest queue: when telemetry cannot be shipped (congestion),
//! the oldest snapshot is discarded so the agent never blocks or grows unbounded.

use std::collections::VecDeque;
use std::sync::Mutex;

pub struct DropOldest<T> {
    cap: usize,
    inner: Mutex<VecDeque<T>>,
}

impl<T> DropOldest<T> {
    pub fn new(cap: usize) -> Self {
        Self {
            cap,
            inner: Mutex::new(VecDeque::with_capacity(cap)),
        }
    }

    /// Push an item; returns the evicted oldest item if the queue was full.
    pub fn push(&self, item: T) -> Option<T> {
        let mut q = self.inner.lock().unwrap();
        let evicted = if q.len() >= self.cap {
            q.pop_front()
        } else {
            None
        };
        q.push_back(item);
        evicted
    }

    pub fn pop(&self) -> Option<T> {
        self.inner.lock().unwrap().pop_front()
    }

    pub fn len(&self) -> usize {
        self.inner.lock().unwrap().len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn evicts_oldest() {
        let q = DropOldest::new(2);
        assert_eq!(q.push(1), None);
        assert_eq!(q.push(2), None);
        assert_eq!(q.push(3), Some(1));
        assert_eq!(q.pop(), Some(2));
        assert_eq!(q.pop(), Some(3));
        assert_eq!(q.len(), 0);
    }
}
