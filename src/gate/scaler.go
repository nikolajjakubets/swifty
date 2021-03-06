/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"fmt"
	"context"
	"sync"
	"time"
)

func condWaitTmo(cond *sync.Cond, tmo time.Duration) {
	d := time.AfterFunc(tmo, func() { cond.Signal() })
	cond.Wait()
	d.Stop()
}

func scalerLog(fdm *FnMemData, msg string) {
	ctx, done := mkContext("::scaler")
	defer done(ctx)

	ctxlog(ctx).Debugf("Scale %s %s to %d", fdm.depname, msg, fdm.bd.goal)
	logSaveEvent(ctx, fdm.fnid, fmt.Sprintf("scale %s -> %d", msg, fdm.bd.goal))
}

func balancerFnScaler(fdm *FnMemData) {
	scalers.Inc()
up:
	scalerLog(fdm, "up")
	goal := k8sDepScaleUp(fdm.depname, fdm.bd.goal)

	fdm.lock.Lock()
	if fdm.bd.goal == 0 {
		goto fin
	}

	if fdm.bd.goal > goal {
		fdm.lock.Unlock()
		goto up
	}
relax:
	condWaitTmo(fdm.bd.wakeup, DepScaleupRelax)

down:
	if fdm.bd.goal <= 1 {
		fdm.bd.wakeup = nil
		goto fin
	}
	if fdm.bd.goal > goal {
		fdm.lock.Unlock()
		goto up
	}

	fdm.bd.goal--
	condWaitTmo(fdm.bd.wakeup, DepScaledownStep)
	if fdm.bd.goal == 0 {
		goto fin
	}
	if fdm.bd.goal == goal {
		goto relax
	}
	if fdm.bd.goal > goal {
		fdm.lock.Unlock()
		goto up
	}

	fdm.lock.Unlock()
	scalerLog(fdm, "down")
	goal = k8sDepScaleDown(fdm.depname, fdm.bd.goal)
	fdm.lock.Lock()

	goto down

fin:
	fdm.lock.Unlock()
	scalerLog(fdm, "fin")
	scalers.Dec()
}

func scalerStop(ctx context.Context, fdm *FnMemData) {
	fdm.lock.Lock()
	if fdm.bd.wakeup != nil {
		fdm.bd.goal = 0
		fdm.bd.wakeup.Signal()
	}
	fdm.lock.Unlock()
}

func scalerSetGoal(ctx context.Context, fdm *FnMemData, goal uint32) {
	scalerGoals.Observe(float64(goal))
	if goal <= fdm.bd.goal {
		return
	}

	fdm.lock.Lock()
	if goal <= fdm.bd.goal {
		fdm.lock.Unlock()
		return
	}

	if goal > uint32(conf.Runtime.MaxReplicas) {
		fdm.lock.Unlock()
		ctxlog(ctx).Debugf("Too many replicas (%d) needed for %s", goal, fdm.depname)
		scaleOverruns.Inc()
		return
	}

	fdm.bd.goal = goal

	if fdm.bd.wakeup == nil {
		fdm.bd.wakeup = sync.NewCond(&fdm.lock)
		go balancerFnScaler(fdm)
	} else {
		fdm.bd.wakeup.Signal()
	}
	fdm.lock.Unlock()
}

func scalerInit(ctx context.Context, fn *FunctionDesc, tgt uint32) error {
	fdm, err := memdGetFn(ctx, fn)
	if err != nil {
		return fmt.Errorf("Can't get fdmd for %s: %s", fn.SwoId.Str(), err.Error())
	}

	fdm.bd.goal = tgt
	fdm.bd.wakeup = sync.NewCond(&fdm.lock)
	go balancerFnScaler(fdm)

	return nil
}
