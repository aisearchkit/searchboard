//! Log tailing helpers. File watching (notify) + inode rotation tracking + .offset
//! checkpoints are TODO; the Java multi-line merger below is the core pure logic.

/// Merges Java stack-trace continuation lines into the preceding event.
/// A new event starts with a "[" timestamp prefix (ES log layout); lines starting with
/// whitespace + "at ", "Caused by:", or "... N more" are continuations.
#[derive(Default)]
pub struct MultilineMerger {
    current: Option<String>,
}

fn is_continuation(line: &str) -> bool {
    let t = line.trim_start();
    line.starts_with(char::is_whitespace)
        || t.starts_with("at ")
        || t.starts_with("Caused by:")
        || t.starts_with("... ")
        || t.starts_with("Suppressed:")
}

impl MultilineMerger {
    /// Feed one line; returns a completed event when a new one begins.
    pub fn push(&mut self, line: &str) -> Option<String> {
        if is_continuation(line) && self.current.is_some() {
            let cur = self.current.as_mut().unwrap();
            cur.push('\n');
            cur.push_str(line);
            None
        } else {
            self.current.replace(line.to_string())
        }
    }

    /// Flush the pending event (call on idle timeout / shutdown).
    pub fn flush(&mut self) -> Option<String> {
        self.current.take()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn merges_stack_trace() {
        let mut m = MultilineMerger::default();
        assert_eq!(m.push("[2026-11-20T08:00:00][INFO ] started"), None);
        assert_eq!(
            m.push("[2026-11-20T08:00:01][ERROR] boom").as_deref(),
            Some("[2026-11-20T08:00:00][INFO ] started")
        );
        assert_eq!(m.push("java.lang.OutOfMemoryError: Java heap space"), Some("[2026-11-20T08:00:01][ERROR] boom".into()));
        assert_eq!(m.push("\tat org.elasticsearch.Foo.bar(Foo.java:1)"), None);
        assert_eq!(m.push("Caused by: x"), None);
        let ev = m.flush().unwrap();
        assert!(ev.contains("OutOfMemoryError") && ev.contains("Foo.bar") && ev.contains("Caused by"));
    }
}
