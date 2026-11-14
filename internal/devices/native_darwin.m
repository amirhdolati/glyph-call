//go:build darwin && cgo

#import <Foundation/Foundation.h>
#import <AVFoundation/AVFoundation.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <AudioToolbox/AudioToolbox.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdbool.h>
#include "native.h"

static void set_error(char *buffer, int size, NSString *message) {snprintf(buffer,size,"%s",message.UTF8String ?: "Unknown device error");}
static BOOL permission(AVMediaType type) {
 AVAuthorizationStatus status=[AVCaptureDevice authorizationStatusForMediaType:type];
 if(status==AVAuthorizationStatusAuthorized)return YES;
 if(status!=AVAuthorizationStatusNotDetermined)return NO;
 dispatch_semaphore_t ready=dispatch_semaphore_create(0);__block BOOL granted=NO;
 [AVCaptureDevice requestAccessForMediaType:type completionHandler:^(BOOL value){granted=value;dispatch_semaphore_signal(ready);}];
 if(dispatch_semaphore_wait(ready,dispatch_time(DISPATCH_TIME_NOW,30*NSEC_PER_SEC))!=0)return NO;
 return granted;
}
static NSArray<AVCaptureDevice *> *cameras(void) {return [AVCaptureDevice devicesWithMediaType:AVMediaTypeVideo];}
int ac_camera_count(void) {@autoreleasepool{return (int)cameras().count;}}
const char *ac_camera_name(int index) {@autoreleasepool{static char name[512];NSArray *items=cameras();if(index<0||index>=items.count)return "Unknown camera";snprintf(name,sizeof(name),"%s",[(AVCaptureDevice*)items[index] localizedName].UTF8String);return name;}}

static pthread_mutex_t camera_lock=PTHREAD_MUTEX_INITIALIZER;
static uint8_t *camera_bytes=NULL;static size_t camera_length=0;static int camera_width=0,camera_height=0,camera_stride=0;
@interface ACCameraOutput:NSObject<AVCaptureVideoDataOutputSampleBufferDelegate>
@end
@implementation ACCameraOutput
-(void)captureOutput:(AVCaptureOutput *)output didOutputSampleBuffer:(CMSampleBufferRef)sample fromConnection:(AVCaptureConnection *)connection {
 CVPixelBufferRef pixel=CMSampleBufferGetImageBuffer(sample);if(!pixel)return;
 CVPixelBufferLockBaseAddress(pixel,kCVPixelBufferLock_ReadOnly);
 size_t width=CVPixelBufferGetWidth(pixel),height=CVPixelBufferGetHeight(pixel),stride=CVPixelBufferGetBytesPerRow(pixel),length=stride*height;
 if(width<=1920&&height<=1080&&length<=1920*1080*4){pthread_mutex_lock(&camera_lock);if(camera_length!=length){free(camera_bytes);camera_bytes=malloc(length);camera_length=length;}if(camera_bytes){memcpy(camera_bytes,CVPixelBufferGetBaseAddress(pixel),length);camera_width=(int)width;camera_height=(int)height;camera_stride=(int)stride;}pthread_mutex_unlock(&camera_lock);}
 CVPixelBufferUnlockBaseAddress(pixel,kCVPixelBufferLock_ReadOnly);
}
@end
static AVCaptureSession *camera_session=nil;static ACCameraOutput *camera_delegate=nil;static dispatch_queue_t camera_queue;
int ac_camera_start(int index,char *error,int error_size) {@autoreleasepool{
 if(!permission(AVMediaTypeVideo)){set_error(error,error_size,@"Camera permission denied. Enable Camera for your terminal in System Settings.");return 0;}
 NSArray *items=cameras();if(index<0||index>=items.count){set_error(error,error_size,@"Camera index not found. Run glyph-call cameras.");return 0;}
 NSError *failure=nil;AVCaptureDeviceInput *input=[AVCaptureDeviceInput deviceInputWithDevice:items[index] error:&failure];if(!input){set_error(error,error_size,failure.localizedDescription);return 0;}
 AVCaptureSession *session=[AVCaptureSession new];[session beginConfiguration];if([session canSetSessionPreset:AVCaptureSessionPreset1280x720])session.sessionPreset=AVCaptureSessionPreset1280x720;else if([session canSetSessionPreset:AVCaptureSessionPreset640x480])session.sessionPreset=AVCaptureSessionPreset640x480;
 if(![session canAddInput:input]){set_error(error,error_size,@"Cannot open camera input.");return 0;}[session addInput:input];
 AVCaptureVideoDataOutput *output=[AVCaptureVideoDataOutput new];output.videoSettings=@{(id)kCVPixelBufferPixelFormatTypeKey:@(kCVPixelFormatType_32BGRA)};output.alwaysDiscardsLateVideoFrames=YES;
 camera_delegate=[ACCameraOutput new];camera_queue=dispatch_queue_create("ascii-call.camera",DISPATCH_QUEUE_SERIAL);[output setSampleBufferDelegate:camera_delegate queue:camera_queue];
 if(![session canAddOutput:output]){set_error(error,error_size,@"Cannot create camera output.");camera_delegate=nil;return 0;}[session addOutput:output];[session commitConfiguration];camera_session=session;[session startRunning];return 1;
}}
int ac_camera_read(void *data,int capacity,int *width,int *height,int *stride){pthread_mutex_lock(&camera_lock);int length=(int)camera_length;if(!camera_bytes||!camera_width||length>capacity){pthread_mutex_unlock(&camera_lock);return 0;}memcpy(data,camera_bytes,length);*width=camera_width;*height=camera_height;*stride=camera_stride;pthread_mutex_unlock(&camera_lock);return length;}
void ac_camera_stop(void){@autoreleasepool{[camera_session stopRunning];if(camera_queue)dispatch_sync(camera_queue,^{});camera_session=nil;camera_delegate=nil;camera_queue=nil;pthread_mutex_lock(&camera_lock);free(camera_bytes);camera_bytes=NULL;camera_length=0;camera_width=0;pthread_mutex_unlock(&camera_lock);}}

#define AUDIO_BYTES 1920
#define RING_SIZE (AUDIO_BYTES*8)
typedef struct {uint8_t bytes[RING_SIZE];size_t head,length;pthread_mutex_t lock;} AudioRing;
static AudioRing input_ring={.lock=PTHREAD_MUTEX_INITIALIZER},output_ring={.lock=PTHREAD_MUTEX_INITIALIZER};
static AudioQueueRef input_queue=NULL,output_queue=NULL;static atomic_bool audio_running=false;
static void ring_put(AudioRing *ring,const void *data,int size){if(size>RING_SIZE){data=(const uint8_t*)data+size-RING_SIZE;size=RING_SIZE;}pthread_mutex_lock(&ring->lock);while(ring->length+size>RING_SIZE){size_t drop=MIN(AUDIO_BYTES,ring->length);ring->head=(ring->head+drop)%RING_SIZE;ring->length-=drop;}for(int i=0;i<size;i++)ring->bytes[(ring->head+ring->length+i)%RING_SIZE]=((const uint8_t*)data)[i];ring->length+=size;pthread_mutex_unlock(&ring->lock);}
static int ring_read(AudioRing *ring,void *data,int size){pthread_mutex_lock(&ring->lock);if(ring->length<size){pthread_mutex_unlock(&ring->lock);return 0;}for(int i=0;i<size;i++)((uint8_t*)data)[i]=ring->bytes[(ring->head+i)%RING_SIZE];ring->head=(ring->head+size)%RING_SIZE;ring->length-=size;pthread_mutex_unlock(&ring->lock);return size;}
static void input_callback(void *context,AudioQueueRef queue,AudioQueueBufferRef buffer,const AudioTimeStamp *time,UInt32 count,const AudioStreamPacketDescription *descriptions){if(!atomic_load(&audio_running))return;ring_put(&input_ring,buffer->mAudioData,(int)buffer->mAudioDataByteSize);AudioQueueEnqueueBuffer(queue,buffer,0,NULL);}
static void output_callback(void *context,AudioQueueRef queue,AudioQueueBufferRef buffer){if(!atomic_load(&audio_running))return;memset(buffer->mAudioData,0,AUDIO_BYTES);ring_read(&output_ring,buffer->mAudioData,AUDIO_BYTES);buffer->mAudioDataByteSize=AUDIO_BYTES;AudioQueueEnqueueBuffer(queue,buffer,0,NULL);}
int ac_audio_start(char *error,int error_size){@autoreleasepool{
 if(!permission(AVMediaTypeAudio)){set_error(error,error_size,@"Microphone permission denied. Enable Microphone for your terminal in System Settings.");return 0;}
 AudioStreamBasicDescription format={.mSampleRate=48000,.mFormatID=kAudioFormatLinearPCM,.mFormatFlags=kAudioFormatFlagIsSignedInteger|kAudioFormatFlagIsPacked,.mBytesPerPacket=2,.mFramesPerPacket=1,.mBytesPerFrame=2,.mChannelsPerFrame=1,.mBitsPerChannel=16};
 OSStatus status=AudioQueueNewInput(&format,input_callback,NULL,NULL,NULL,0,&input_queue);if(status==0)status=AudioQueueNewOutput(&format,output_callback,NULL,NULL,NULL,0,&output_queue);
 if(status!=0){snprintf(error,error_size,"Cannot open microphone/speaker (CoreAudio %d)",(int)status);ac_audio_stop();return 0;}
 atomic_store(&audio_running,true);
 for(int i=0;i<3;i++){AudioQueueBufferRef in,out;status=AudioQueueAllocateBuffer(input_queue,AUDIO_BYTES,&in);if(status==0)status=AudioQueueEnqueueBuffer(input_queue,in,0,NULL);if(status==0)status=AudioQueueAllocateBuffer(output_queue,AUDIO_BYTES,&out);if(status!=0)break;memset(out->mAudioData,0,AUDIO_BYTES);out->mAudioDataByteSize=AUDIO_BYTES;status=AudioQueueEnqueueBuffer(output_queue,out,0,NULL);if(status!=0)break;}
 if(status==0)status=AudioQueueStart(output_queue,NULL);if(status==0)status=AudioQueueStart(input_queue,NULL);if(status!=0){snprintf(error,error_size,"Cannot start audio (CoreAudio %d)",(int)status);ac_audio_stop();return 0;}return 1;
}}
int ac_audio_read(void *data,int size){return ring_read(&input_ring,data,size);}
void ac_audio_write(const void *data,int size){ring_put(&output_ring,data,size);}
void ac_audio_stop(void){atomic_store(&audio_running,false);if(input_queue){AudioQueueStop(input_queue,true);AudioQueueDispose(input_queue,true);input_queue=NULL;}if(output_queue){AudioQueueStop(output_queue,true);AudioQueueDispose(output_queue,true);output_queue=NULL;}pthread_mutex_lock(&input_ring.lock);input_ring.head=input_ring.length=0;pthread_mutex_unlock(&input_ring.lock);pthread_mutex_lock(&output_ring.lock);output_ring.head=output_ring.length=0;pthread_mutex_unlock(&output_ring.lock);}
