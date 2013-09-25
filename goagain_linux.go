package gop

import (
    "github.com/rcrowley/goagain"
    "net"
    "time"
)

func (a *App) goAgainSetup() {
    goagain.OnSIGUSR1 = func(l net.Listener) error {
        a.Info("SIGUSR1 received")
        return nil
    }
}

func (a *App) goAgainListenAndServe(listenNet, listenAddr string) {
	l, ppid, err := goagain.GetEnvs()

    if err != nil {
        a.Info("No parent - starting listener on %s:%s", listenNet, listenAddr)
        // No parent, start our own listener
        l, err = net.Listen(listenNet, listenAddr)
        a.Debug("Listener is %v err is %v", l, err)
		if err != nil {
			a.Fatalln(err)
		}
    } else {
        // Parent! Familicide
        a.Info("Child taking over from graceful parent. Killing ppid %d\n", ppid)
		if err := goagain.KillParent(ppid); nil != err {
			a.Fatalln(err)
		}
    }
    go func() {
        a.Serve(l)
    }()

	// Block the main goroutine awaiting signals.
	if err := goagain.AwaitSignals(l); nil != err {
		a.Fatalln(err)
    }

    a.Error("Signal received - starting graceful restart")
    waitSecs, _ := a.Cfg.GetInt("gop", "graceful_wait_secs", 60)
    timeoutChan := time.After(time.Second * time.Duration(waitSecs))

    tickMillis, _ := a.Cfg.GetInt("gop", "graceful_poll_msecs", 500)
    tickChan := time.Tick(time.Millisecond * time.Duration(tickMillis))

    waiting := true
    for waiting {
        select {
            case <- timeoutChan: {
                a.Error("Graceful restart timed out after %d seconds - being less graceful and exiting", waitSecs)
                waiting = false
            }
            case <- tickChan: {
                if a.currentReqs == 0 {
                    a.Error("Graceful restart - no pending requests - time to die")
                    waiting = false
                } else {
                    a.Info("Graceful restart - tick still have %d pending reqs", a.currentReqs)
                }
            }
        }
    }

    a.Info("Graceful restart - tick still have %d pending reqs", a.currentReqs)
    // Wait for graceful exit...
}

