package installer

import "fmt"

type BinaryDownloadResponseDecision struct {
	Accept bool
	Retry  bool
	Err    error
}

type BinaryDownloadResponsePolicy struct{}

func NewBinaryDownloadResponsePolicy() BinaryDownloadResponsePolicy {
	return BinaryDownloadResponsePolicy{}
}

func (BinaryDownloadResponsePolicy) Decide(statusCode int, status string) BinaryDownloadResponseDecision {
	if statusCode >= 500 {
		return BinaryDownloadResponseDecision{Retry: true, Err: fmt.Errorf("download failed: %s", status)}
	}
	if statusCode < 200 || statusCode >= 300 {
		return BinaryDownloadResponseDecision{Err: fmt.Errorf("download failed: %s", status)}
	}
	return BinaryDownloadResponseDecision{Accept: true}
}
