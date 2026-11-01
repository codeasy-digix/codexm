//! Crossterm may wait inside its reader after a PTY loses its master. Keep the
//! UI/signal loop independent so closing a terminal can still reap our bridge.
use crossterm::event::{self, Event};
use std::io;
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
    mpsc::{self, Receiver, RecvTimeoutError},
};
use std::time::Duration;

pub struct Events {
    receiver: Receiver<io::Result<Event>>,
    stopped: Arc<AtomicBool>,
}
impl Events {
    pub fn start() -> Self {
        let (sender, receiver) = mpsc::sync_channel(32);
        let stopped = Arc::new(AtomicBool::new(false));
        let stop = stopped.clone();
        std::thread::spawn(move || {
            while !stop.load(Ordering::Relaxed) {
                match event::poll(Duration::from_millis(100)) {
                    Ok(false) => continue,
                    Ok(true) => {
                        if stop.load(Ordering::Relaxed) {
                            break;
                        }
                        let value = event::read();
                        let failed = value.is_err();
                        if sender.send(value).is_err() || failed {
                            break;
                        }
                    }
                    Err(e) => {
                        let _ = sender.send(Err(e));
                        break;
                    }
                }
            }
        });
        Self { receiver, stopped }
    }
    pub fn next(&self) -> io::Result<Option<Event>> {
        match self.receiver.recv_timeout(Duration::from_millis(100)) {
            Ok(value) => value.map(Some),
            Err(RecvTimeoutError::Timeout) => Ok(None),
            Err(RecvTimeoutError::Disconnected) => Err(io::Error::other("terminal input closed")),
        }
    }
}
impl Drop for Events {
    fn drop(&mut self) {
        self.stopped.store(true, Ordering::Relaxed);
        // Do not join a reader stuck on a closed PTY. This foreground process
        // exits or execs immediately after reaping its owned bridge.
    }
}
