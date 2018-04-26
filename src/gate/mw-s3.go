package main

import (
	"fmt"
	"context"
	"strings"
	"net/http"
	"encoding/json"
	"gopkg.in/mgo.v2/bson"
	"../common"
	"../common/http"
	"../apis/apps"
	"../apis/apps/s3"
)

func s3KeyGen(conf *YAMLConfS3, namespace, bucket string, lifetime uint32) (string, string, error) {
	addr := conf.c.AddrP(conf.AdminPort)

	resp, err := swyhttp.MarshalAndPost(
		&swyhttp.RestReq{
			Address: "http://" + addr + "/v1/api/admin/keygen",
			Timeout: 120,
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.c.Pass]},
		},
		&swys3api.S3CtlKeyGen{
			Namespace: namespace,
			Bucket: bucket,
			Lifetime: lifetime,
		})
	if err != nil {
		return "", "", fmt.Errorf("Error requesting NS from S3: %s", err.Error())
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Bad responce from S3 gate: %s", string(resp.Status))
	}

	var out swys3api.S3CtlKeyGenResult

	err = swyhttp.ReadAndUnmarshalResp(resp, &out)
	if err != nil {
		return "", "", fmt.Errorf("Error reading responce from S3: %s", err.Error())
	}

	return out.AccessKeyID, out.AccessKeySecret, nil
}

func s3KeyDel(conf *YAMLConfS3, key string) error {
	addr := conf.c.AddrP(conf.AdminPort)

	_, err := swyhttp.MarshalAndPost(
		&swyhttp.RestReq{
			Address: "http://" + addr + "/v1/api/admin/keydel",
			Timeout: 120,
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.c.Pass]},
		},
		&swys3api.S3CtlKeyDel{
			AccessKeyID: key,
		})
	if err != nil {
		return fmt.Errorf("Error deleting key from S3: %s", err.Error())
	}

	return nil
}

func InitS3(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) (error) {
	return fmt.Errorf("S3 mware is external")
}

func FiniS3(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) error {
	return fmt.Errorf("S3 mware is external")
}

const (
	gates3queue = "events"
)

func s3Subscribe(ctx context.Context, conf *YAMLConfMw, evt *FnEventS3) error {
	addr := conf.S3.c.AddrP(conf.S3.AdminPort)

	_, err := swyhttp.MarshalAndPost(
		&swyhttp.RestReq{
			Address: "http://" + addr + "/v1/api/notify/subscribe",
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.S3.c.Pass]},
			Success: http.StatusAccepted,
		},
		&swys3api.S3Subscribe{
			Namespace: evt.Ns,
			Bucket: evt.Bucket,
			Ops: evt.Ops,
			Queue: gates3queue,
		})
	if err != nil {
		return fmt.Errorf("Error subscibing: %s", err.Error())
	}

	return nil
}

func s3Unsubscribe(ctx context.Context, conf *YAMLConfMw, evt *FnEventS3) error {
	addr := conf.S3.c.AddrP(conf.S3.AdminPort)

	_, err := swyhttp.MarshalAndPost(
		&swyhttp.RestReq{
			Address: "http://" + addr + "/v1/api/notify/unsubscribe",
			Headers: map[string]string{"X-SwyS3-Token": gateSecrets[conf.S3.c.Pass]},
			Success: http.StatusAccepted,
		},
		&swys3api.S3Subscribe{
			Namespace: evt.Ns,
			Bucket: evt.Bucket,
			Ops: evt.Ops,
		})
	if err != nil {
		ctxlog(ctx).Errorf("Error unsubscibing: %s", err.Error())
	}
	return err
}

func handleS3Event(ctx context.Context, user string, data []byte) {
	var evt swys3api.S3Event

	err := json.Unmarshal(data, &evt)
	if err != nil {
		ctxlog(ctx).Errorf("Invalid event from S3")
		return
	}

	evs, err := dbListEvents(bson.M{"source":"s3", "s3.ns": evt.Namespace, "s3.bucket": evt.Bucket})
	if err != nil {
		/* FIXME -- this should be notified? Or what? */
		ctxlog(ctx).Errorf("mq: Can't list triggers for s3 event")
		return
	}

	for _, ed := range evs {
		if !ed.S3.hasOp(evt.Op) {
			continue
		}

		fn, err := dbFuncFindByCookie(ed.FnId)
		if err != nil || fn == nil {
			continue
		}

		if fn.State != swy.DBFuncStateRdy {
			continue
		}

		/* FIXME -- this is synchronous */
		_, err = doRun(ctx, fn, "s3:" + evt.Op + ":" + evt.Bucket,
				map[string]string {
					"bucket": evt.Bucket,
					"object": evt.Object,
					"op": evt.Op,
				})
		if err != nil {
			ctxlog(ctx).Errorf("s3: Error running FN %s", err.Error())
		}
	}
}

func s3EventStart(ctx context.Context, fn *FunctionDesc, evt *FnEventDesc) error {
	evt.S3.Ns = fn.SwoId.Namespace()
	conf := &conf.Mware
	err := mqStartListener(conf.S3.cn.User, conf.S3.cn.Pass,
		conf.S3.cn.Addr() + "/" + conf.S3.cn.Domn,
		gates3queue, handleS3Event)
	if err == nil {
		err = s3Subscribe(ctx, conf, evt.S3)
		if err != nil {
			mqStopListener(conf.S3.cn.Addr() + "/" + conf.S3.cn.Domn, gates3queue)
		}
	}

	return err
}

func s3EventStop(ctx context.Context, evt *FnEventDesc) error {
	conf := &conf.Mware
	err := s3Unsubscribe(ctx, conf, evt.S3)
	if err == nil {
		mqStopListener(conf.S3.cn.Addr() + "/" + conf.S3.cn.Domn, "events")
	}
	return err
}

func makeS3Envs(conf *YAMLConfS3, bucket, key, skey string) [][2]string {
	var ret [][2]string
	ret = append(ret, mkEnvId(bucket, "s3", "ADDR", conf.c.Addr()))
	ret = append(ret, mkEnvId(bucket, "s3", "KEY", key))
	ret = append(ret, mkEnvId(bucket, "s3", "SECRET", skey))
	return ret
}

func GenBucketKeysS3(ctx context.Context, conf *YAMLConfMw, fid *SwoId, bucket string) ([][2]string, error) {
	var key, skey string
	var err error

	key, skey, err = s3KeyGen(&conf.S3, fid.Namespace(), bucket, 0)
	if err != nil {
		ctxlog(ctx).Errorf("Error generating key for %s/%s: %s", fid.Str(), bucket, err.Error())
		return nil, fmt.Errorf("Key generation error")
	}

	return makeS3Envs(&conf.S3, bucket, key, skey), nil
}

func mwareGetS3Creds(ctx context.Context, conf *YAMLConf, acc *swyapi.MwareS3Access) (*swyapi.MwareS3Creds, *swyapi.GateErr) {
	creds := &swyapi.MwareS3Creds{}

	/* XXX -- for now pretend, that s3 listens on the same address as gate does */
	gateAP := strings.Split(conf.Daemon.Addr, ":")
	creds.Endpoint = gateAP[0] + ":" + conf.Mware.S3.c.Port

	creds.Expires = acc.Lifetime

	for _, acc := range(acc.Access) {
		if acc == "hidden" {
			creds.Expires = conf.Mware.S3.HiddenKeyTmo
			continue
		}

		return nil, GateErrM(swy.GateBadRequest, "Unknown access option " + acc)
	}

	if creds.Expires == 0 {
		return nil, GateErrM(swy.GateBadRequest, "Perpetual keys not allowed")
	}

	var err error
	id := makeSwoId(fromContext(ctx).Tenant, acc.Project, "")
	creds.Key, creds.Secret, err = s3KeyGen(&conf.Mware.S3, id.Namespace(), acc.Bucket, creds.Expires)
	if err != nil {
		ctxlog(ctx).Errorf("Can't get S3 keys for %s.%s", id.Str(), acc.Bucket, err.Error())
		return nil, GateErrM(swy.GateGenErr, "Error getting S3 keys")
	}

	return creds, nil
}

var MwareS3 = MwareOps {
	Init:		InitS3,
	Fini:		FiniS3,
	GenSec:		GenBucketKeysS3,
}
