package main

import (
	"crypto/sha256"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const S3TimeStampMax = int64(0x7fffffffffffffff)

func current_timestamp() int64 {
	return time.Now().Unix()
}

func base64_encode(s []byte) string {
	return base64.StdEncoding.EncodeToString(s)
}

func base64_decode(s string) []byte {
	d, _ := base64.StdEncoding.DecodeString(s)
	return d
}

func md5sum(s []byte) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func sha256sum(s []byte) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func getURLParam(r *http.Request, param string) (string, bool) {
	if v, ok := r.URL.Query()[param]; ok {
		if len(v) > 0 {
			return v[0], true
		} else {
			return "", true
		}
	}
	return "", false
}

func getURLValue(r *http.Request, param string) (string) {
	val, _ := getURLParam(r, param)
	return val
}

func getURLBool(r *http.Request, param string) (bool) {
	val, _ := getURLParam(r, param)
	if strings.ToLower(val) == "true" { return true }
	return false
}

func HTTPMarshalXMLAndWrite(w http.ResponseWriter, status int, data interface{}) error {
	xdata, err := xml.Marshal(data)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(status)
	w.Write(xdata)
	return nil
}

func HTTPMarshalXMLAndWriteOK(w http.ResponseWriter, data interface{}) error {
	return HTTPMarshalXMLAndWrite(w, http.StatusOK, data)
}

func HTTPRespXML(w http.ResponseWriter, data interface{}) {
	err := HTTPMarshalXMLAndWrite(w, http.StatusOK, data)
	if err != nil {
		HTTPRespError(w, S3ErrInternalError, err.Error())
	}
}
