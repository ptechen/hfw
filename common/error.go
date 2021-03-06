package common

import (
	"fmt"
	"runtime"
	"strings"
)

var errorMap = map[int64]string{
	400: "request error",
	500: "system error",
}

func SetErrorMap(m map[int64]string) {
	for k, v := range m {
		errorMap[k] = v
	}
}

func AddErrorMap(errNo int64, errMsg string) {
	errorMap[errNo] = errMsg
}

func GetErrorMap(errNo int64) string {
	return errorMap[errNo]
}

type RespErr struct {
	file   string
	line   int
	errNo  int64
	errMsg string
}

func (respErr *RespErr) ErrNo() int64 {
	if respErr == nil {
		return 0
	}
	return respErr.errNo
}

func (respErr *RespErr) ErrMsg() string {
	if respErr == nil {
		return ""
	}
	return respErr.errMsg
}

func (respErr *RespErr) Error() string {
	return respErr.String()
}

func (respErr *RespErr) String() string {
	if respErr == nil {
		return ""
	}
	return fmt.Sprintf("[RespErr %s:%d N:%d M:%s]",
		respErr.file, respErr.line, respErr.errNo, respErr.errMsg)
}

//记录调用本函数的位置
func NewRespErr(errNo int64, i interface{}) (respErr *RespErr) {
	if errNo == 0 || i == nil {
		return nil
	}
	if r, ok := i.(*RespErr); ok && r != nil {
		r.errNo = errNo
		return r
	}
	respErr = &RespErr{
		errNo:  errNo,
		errMsg: fmt.Sprintf("%v", i),
	}
	_, respErr.file, respErr.line, _ = runtime.Caller(1)
	if GOPATH != "" {
		respErr.file = strings.Replace(respErr.file, GOPATH, "", 1)
	}

	return
}
