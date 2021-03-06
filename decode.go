package minimp3

/*
#define MINIMP3_IMPLEMENTATION
#define MINIMP3_FLOAT_OUTPUT

#include "minimp3.h"
#include <stdlib.h>
#include <stdio.h>

int decode(mp3dec_t *dec, mp3dec_frame_info_t *info, unsigned char *data, int *length, unsigned char *decoded, int *decoded_length) {
    int samples;
    mp3d_sample_t pcm[MINIMP3_MAX_SAMPLES_PER_FRAME];
    samples = mp3dec_decode_frame(dec, data, *length, pcm, info);
    *decoded_length = samples * info->channels * sizeof(mp3d_sample_t);
    *length -= info->frame_bytes;
    unsigned char buffer[*decoded_length];
    memcpy(buffer, (unsigned char*)&(pcm), *decoded_length);
    memcpy(decoded, buffer, *decoded_length);
    return info->frame_bytes;
}
*/
import "C"
import (
	"context"
	"io"
	"sync"
	"time"
	"unsafe"
	"fmt"
)

const maxSamplesPerFrame = 1152 * 2
const pointSize = 4 // float32 4  int16 2
// Decoder decode the mp3 stream by minimp3
type Decoder struct {
	readerLocker  *sync.Mutex
	data          []byte
	decoderLocker *sync.Mutex
	decodedData   []byte
	decode        C.mp3dec_t
	info          C.mp3dec_frame_info_t
	context       context.Context
	contextCancel context.CancelFunc
	SampleRate    int
	Channels      int
	Kbps          int
	Layer         int
}

// BufferSize Decoded data buffer size.
var BufferSize = 1024 * 100

// WaitForDataDuration wait for the data time duration.
var WaitForDataDuration = time.Millisecond * 10

// NewDecoder decode mp3 stream and get a Decoder for read the raw data to play.
func NewDecoder(reader io.Reader) (dec *Decoder, err error) {
	dec = new(Decoder)
	dec.readerLocker = new(sync.Mutex)
	dec.decoderLocker = new(sync.Mutex)
	dec.context, dec.contextCancel = context.WithCancel(context.Background())
	dec.decode = C.mp3dec_t{}
	C.mp3dec_init(&dec.decode)
	dec.info = C.mp3dec_frame_info_t{}
	go func() {
		for {
			select {
			case <-dec.context.Done():
				return
			default:
			}
			if len(dec.data) > BufferSize {
				<-time.After(WaitForDataDuration)
				continue
			}
			var data = make([]byte, 512)
			var n int
			n, err = reader.Read(data)

			dec.readerLocker.Lock()
			dec.data = append(dec.data, data[:n]...)
			dec.readerLocker.Unlock()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
		}
	}()
	go func() {
		for {
			select {
			case <-dec.context.Done():
				return
			default:
			}
			if len(dec.decodedData) > BufferSize {
				<-time.After(WaitForDataDuration)
				continue
			}
			var decoded = [maxSamplesPerFrame * pointSize]byte{}
			var decodedLength = C.int(0)
			var length = C.int(len(dec.data))
			if len(dec.data) < 960 {//at least a frame
				<-time.After(WaitForDataDuration)
				continue
			}
			frameSize := C.decode(&dec.decode, &dec.info,
				(*C.uchar)(unsafe.Pointer(&dec.data[0])),
				&length, (*C.uchar)(unsafe.Pointer(&decoded[0])),
				&decodedLength)
			if int(frameSize) == 0 {
				<-time.After(WaitForDataDuration)
				continue
			} else if int(frameSize) == -1{
				fmt.Println("decode error")
				<-time.After(WaitForDataDuration)
				continue
			}
			dec.SampleRate = int(dec.info.hz)
			dec.Channels = int(dec.info.channels)
			dec.Kbps = int(dec.info.bitrate_kbps)
			dec.Layer = int(dec.info.layer)
			dec.readerLocker.Lock()
			dec.decoderLocker.Lock()
			dec.decodedData = append(dec.decodedData, decoded[:decodedLength]...)
			if int(frameSize) < len(dec.data) {
				dec.data = dec.data[int(frameSize):]
			}
			dec.decoderLocker.Unlock()
			dec.readerLocker.Unlock()
		}
	}()
	return
}

// Started check the record mp3 stream started ot not.
func (dec *Decoder) Started() (channel chan bool) {
	channel = make(chan bool)
	go func() {
		for {
			select {
			case <-dec.context.Done():
				channel <- false
			default:
			}
			if len(dec.decodedData) != 0 {
				channel <- true
			} else {
				<-time.After(time.Millisecond * 100)
			}
		}
	}()
	return
}

// Read read the raw stream
func (dec *Decoder) Read(data []byte) (n int, err error) {
	dec.decoderLocker.Lock()
	defer dec.decoderLocker.Unlock()
	if len(dec.decodedData) == 0 {
		err = io.EOF
		return
	}
	n = copy(data, dec.decodedData[:])
	dec.decodedData = dec.decodedData[n:]
	return
}

// Close stop the decode mp3 stream cycle.
func (dec *Decoder) Close() {
	if dec.contextCancel != nil {
		dec.contextCancel()
	}
}

// DecodeFull put all of the mp3 data to decode.
func DecodeFull(mp3 []byte) (dec *Decoder, decodedData []byte, err error) {
	dec = new(Decoder)
	dec.decode = C.mp3dec_t{}
	C.mp3dec_init(&dec.decode)
	info := C.mp3dec_frame_info_t{}
	var length = C.int(len(mp3))
	for {
		var decoded = [maxSamplesPerFrame * pointSize]byte{}
		var decodedLength = C.int(0)
		frameSize := C.decode(&dec.decode,
			&info, (*C.uchar)(unsafe.Pointer(&mp3[0])),
			&length, (*C.uchar)(unsafe.Pointer(&decoded[0])),
			&decodedLength)
		if int(frameSize) == 0 {
			break
		}
		decodedData = append(decodedData, decoded[:decodedLength]...)
		if int(frameSize) < len(mp3) {
			mp3 = mp3[int(frameSize):]
		}
		dec.SampleRate = int(info.hz)
		dec.Channels = int(info.channels)
		dec.Kbps = int(info.bitrate_kbps)
		dec.Layer = int(info.layer)
	}
	return
}
