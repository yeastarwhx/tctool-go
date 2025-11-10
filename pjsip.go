package main

/*
#cgo CFLAGS: -I /home/test/tools/ctool/include -I /home/test/tools/ctool/include/include
#cgo LDFLAGS: -L${SRCDIR}/../ctool/lib -lyspjsua -lpjsua -lpjsip -lpjmedia -lpjnath -lpjlib-util -lpj -lssl -lcrypto -lpthread

#include <stdlib.h>
#include <string.h>
#include "ys_pjsua_app.h"

// Status type for return values
typedef int pj_status_t;
#define PJ_SUCCESS 0
*/
import "C"
import (
	"errors"
	"unsafe"
)

const (
	PJSuccess = C.PJ_SUCCESS
)

// InitPJSUA initializes PJSIP library
func InitPJSUA(cfgName *string, codec string) error {
	var cCfgName *C.char
	if cfgName != nil {
		cCfgName = C.CString(*cfgName)
		defer C.free(unsafe.Pointer(cCfgName))
	}

	cCodec := C.CString(codec)
	defer C.free(unsafe.Pointer(cCodec))

	var cb C.ys_pjsua_callback
	result := C.ys_init_pjsua(cCfgName, cb, cCodec)
	if int(result) != 0 {
		return errors.New("failed to initialize PJSUA")
	}
	return nil
}

// ParseCallID extracts Call-ID from SIP message
func ParseCallID(sipMsg string) (string, error) {
	cSipMsg := C.CString(sipMsg)
	defer C.free(unsafe.Pointer(cSipMsg))

	callIDBuf := make([]byte, 256)
	cCallIDBuf := (*C.char)(unsafe.Pointer(&callIDBuf[0]))

	status := C.ys_parse_call_id(
		cSipMsg,
		C.pj_size_t(len(sipMsg)),
		cCallIDBuf,
		C.pj_size_t(len(callIDBuf)),
	)

	if int(status) != PJSuccess {
		return "", errors.New("failed to parse Call-ID")
	}

	return C.GoString(cCallIDBuf), nil
}

// SDPInfo contains SDP information extracted from SIP message
type SDPInfo struct {
	AudioRTP       int
	AudioRTCP      int
	VideoRTP       int
	VideoRTCP      int
	ConnectionAddr string
}

// ParseSDPInfo extracts SDP information from SIP message
func ParseSDPInfo(sipMsg string) (*SDPInfo, error) {
	cSipMsg := C.CString(sipMsg)
	defer C.free(unsafe.Pointer(cSipMsg))

	var audioRTP, audioRTCP, videoRTP, videoRTCP C.int
	sdpAddrBuf := make([]byte, 64)
	cSDPAddr := (*C.char)(unsafe.Pointer(&sdpAddrBuf[0]))

	status := C.ys_parse_sdp_info(
		cSipMsg,
		C.pj_size_t(len(sipMsg)),
		&audioRTP,
		&audioRTCP,
		&videoRTP,
		&videoRTCP,
		cSDPAddr,
		C.pj_size_t(len(sdpAddrBuf)),
	)

	if int(status) != PJSuccess {
		return nil, errors.New("failed to parse SDP info")
	}

	return &SDPInfo{
		AudioRTP:       int(audioRTP),
		AudioRTCP:      int(audioRTCP),
		VideoRTP:       int(videoRTP),
		VideoRTCP:      int(videoRTCP),
		ConnectionAddr: C.GoString(cSDPAddr),
	}, nil
}

// ModifySIPPacket modifies SIP packet's Contact and SDP fields
func ModifySIPPacket(
	sipMsg string,
	contactHost *string,
	contactPort int,
	audioRTP int,
	audioRTCP int,
	videoRTP int,
	videoRTCP int,
	sdpAddr string,
) (string, error) {
	cSipMsg := C.CString(sipMsg)
	defer C.free(unsafe.Pointer(cSipMsg))

	var cContactHost *C.char
	if contactHost != nil {
		cContactHost = C.CString(*contactHost)
		defer C.free(unsafe.Pointer(cContactHost))
	}

	cSDPAddr := C.CString(sdpAddr)
	defer C.free(unsafe.Pointer(cSDPAddr))

	outputBuf := make([]byte, 4096)
	cOutputBuf := (*C.char)(unsafe.Pointer(&outputBuf[0]))

	status := C.ys_modify_sip_packet(
		cSipMsg,
		C.pj_size_t(len(sipMsg)),
		cOutputBuf,
		C.pj_size_t(len(outputBuf)),
		cContactHost,
		C.int(contactPort),
		C.int(audioRTP),
		C.int(audioRTCP),
		C.int(videoRTP),
		C.int(videoRTCP),
		cSDPAddr,
	)

	if int(status) != PJSuccess {
		return "", errors.New("failed to modify SIP packet")
	}

	return C.GoString(cOutputBuf), nil
}

// DestroyPJSUA cleans up PJSIP library
func DestroyPJSUA() error {
	result := C.ys_destroy_pjsua()
	if int(result) != 0 {
		return errors.New("failed to destroy PJSUA")
	}
	return nil
}
