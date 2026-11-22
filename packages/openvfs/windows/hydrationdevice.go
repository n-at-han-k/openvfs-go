//go:build windows

package windows

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"unsafe"

	cf "github.com/deploymenttheory/go-bindings-win32/bindings/win32/storage/cloudfilters"
)

// Transpiled from src/plugins/vfs/cfapi/hydrationdevice.{h,cpp}.

const (
	// ChunkSize is 4_KiB.
	ChunkSize = 4 * 1024
	// BufferSize is 4_MiB.
	BufferSize = 4 * 1024 * 1024
)

// HydrationJob is the part of OCC::HydrationJob (libsync/vfs/hydrationjob.h)
// the cfapi plugin drives. The download itself is a GETFileJob in libsync; here
// it is the Fetch function the owner installs, which writes the body into the
// device exactly as the QIODevice pipeline does.
type HydrationJob struct {
	vfs    *VfsCfApi
	fileID []byte
	device *HydrationDevice
	// Fetch streams the remote contents into the device. It stands in for
	// HydrationJob::start()'s GETFileJob.
	Fetch func(fileID []byte, device *HydrationDevice) error

	fileName string
	record   SyncFileItem

	// OnFinished and OnError are the finished() and error() signals.
	OnFinished func()
	OnError    func(err string)

	aborted chan struct{}
	once    sync.Once
}

// CfApiHydrationJob is the class of the same name: a HydrationJob that also
// remembers the callback context it was created for.
type CfApiHydrationJob struct {
	*HydrationJob
	context CallBackContext
}

func (j *CfApiHydrationJob) SetContext(context CallBackContext) { j.context = context }

func (j *CfApiHydrationJob) Context() CallBackContext { return j.context }

func NewHydrationJob(v *VfsCfApi, fileID []byte, device *HydrationDevice) *HydrationJob {
	return &HydrationJob{vfs: v, fileID: fileID, device: device, aborted: make(chan struct{})}
}

func (j *HydrationJob) Vfs() *VfsCfApi { return j.vfs }

func (j *HydrationJob) FileID() []byte { return j.fileID }

func (j *HydrationJob) Device() *HydrationDevice { return j.device }

func (j *HydrationJob) Record() SyncFileItem { return j.record }

// SetTargetFile is HydrationJob::setTargetFile().
func (j *HydrationJob) SetTargetFile(fileName string) { j.fileName = fileName }

func (j *HydrationJob) TargetFileName() string { return j.fileName }

// Start is HydrationJob::start().
func (j *HydrationJob) Start() {
	go func() {
		if j.Fetch == nil {
			j.emitError("no hydration transport installed")
			return
		}
		if err := j.Fetch(j.fileID, j.device); err != nil {
			j.emitError(err.Error())
			return
		}
		j.emitFinished()
	}()
}

// Abort is HydrationJob::abort().
func (j *HydrationJob) Abort() {
	j.once.Do(func() {
		close(j.aborted)
		j.emitError("aborted")
	})
}

// Aborted reports whether Abort has been called, so a Fetch can stop early.
func (j *HydrationJob) Aborted() <-chan struct{} { return j.aborted }

func (j *HydrationJob) emitFinished() {
	if j.OnFinished != nil {
		j.OnFinished()
	}
}

func (j *HydrationJob) emitError(err string) {
	if j.OnError != nil {
		j.OnError(err)
	}
}

// HydrationDevice is the QIODevice the downloaded bytes are written into. It
// forwards them to the cloud filter in ChunkSize aligned blocks.
type HydrationDevice struct {
	context CallBackContext
	// totalSize is the expected total size.
	totalSize int64
	// offset is the current offset.
	offset int64

	buffer []byte

	errorString string
}

// RequestHydration is HydrationDevice::requestHydration().
func RequestHydration(context CallBackContext, totalSize int64) *CfApiHydrationJob {
	lcCfApiHydrationDevice.Info("Requesting hydration %s", context)
	v := context.Vfs
	if v == nil {
		return nil
	}
	if _, running := v.hydrationJobs[context.TransferKey]; running {
		lcCfApiHydrationDevice.Warning("Ignoring hydration request for running hydration %s", context)
		return nil
	}
	// we assume that transferKey is unique and that we don't receive multiple
	// requests for the same file with the same key
	for _, it := range v.hydrationJobs {
		if it.Context().Path == context.Path {
			lcCfApiHydrationDevice.Warning("A hydration for %s is already running", context.Path)
			return nil
		}
	}

	device := NewHydrationDevice(context, totalSize)
	hydration := &CfApiHydrationJob{HydrationJob: NewHydrationJob(v, context.FileID, device)}
	hydration.SetContext(context)
	v.hydrationJobs[context.TransferKey] = hydration

	hydration.OnFinished = func() {
		v.post(func() {
			delete(v.hydrationJobs, hydration.Context().TransferKey)
			if _, err := os.Stat(hydration.Context().Path); err == nil {
				item := hydration.Record()
				// the file is now downloaded
				item.Type = ItemTypeFile
				if inode, ok := getInode(hydration.Context().Path); ok {
					item.Inode = inode
				} else {
					lcCfApiHydrationDevice.Warning("Failed to get inode for %s", hydration.Context().Path)
				}
				if v.params.Journal == nil {
					return
				}
				if err := v.params.Journal.SetFileRecord(item); err != nil {
					lcCfApiHydrationDevice.Warning("Error when setting the file record to the database %s %v",
						hydration.Context(), err)
				} else {
					lcCfApiHydrationDevice.Info("Hydration succeeded %s", hydration.Context())
				}
			} else {
				lcCfApiHydrationDevice.Warning("Hydration succeeded but the file appears to be moved %s", hydration.Context())
			}
		})
	}

	hydration.OnError = func(errStr string) {
		v.post(func() {
			delete(v.hydrationJobs, hydration.Context().TransferKey)
			lcCfApiHydrationDevice.Warning("Hydration failed %s %s", hydration.Context(), errStr)

			opInfo := cf.CF_OPERATION_INFO{}
			opInfo.StructSize = uint32(unsafe.Sizeof(opInfo))
			opInfo.Type = cf.CF_OPERATION_TYPE_TRANSFER_DATA

			opInfo.ConnectionKey = hydration.Context().ConnectionKey
			opInfo.TransferKey = hydration.Context().TransferKey

			opParams := cf.CF_OPERATION_PARAMETERS{}
			opParams.ParamSize = sizeOfOpParamTransferData
			transfer := transferDataArm(&opParams)
			transfer.CompletionStatus = STATUS_CLOUD_FILE_NOT_IN_SYNC

			// the error affects the whole file, not a transfer chunk
			transfer.Offset = 0
			transfer.Length = totalSize

			if err := cf.CfExecute(&opInfo, &opParams); err != nil {
				lcCfApiHydrationDevice.Critical("Couldn't report error %s : %v",
					hydration.Context(), err)
			}
		})
	}

	return hydration
}

func NewHydrationDevice(context CallBackContext, totalSize int64) *HydrationDevice {
	return &HydrationDevice{
		context:   context,
		totalSize: totalSize,
		// we reserve a fixed size and don't expect to ever grow the array
		buffer: make([]byte, 0, BufferSize),
	}
}

// ReadData is HydrationDevice::readData(), which upstream marks unreachable.
func (d *HydrationDevice) ReadData(p []byte) (int, error) {
	return 0, fmt.Errorf("HydrationDevice is write only")
}

// Write is HydrationDevice::writeData(). It keeps the io.Writer contract so a
// download can be streamed straight into it.
func (d *HydrationDevice) Write(data []byte) (int, error) {
	n, err := d.writeData(data)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (d *HydrationDevice) writeData(data []byte) (int, error) {
	length := len(data)
	d.buffer = append(d.buffer, data...)
	isLastChunk := (d.offset + int64(len(d.buffer))) >= d.totalSize
	// only write if the buffer is decently filled, or we are in the last chunk
	if len(d.buffer) >= BufferSize*9/10 || isLastChunk {
		// ensure we chunk the writes to the block size, if we are at then end
		// of the file take all the rest
		var currentBlockLength int
		if isLastChunk {
			// we are in the last chunk, use everything
			currentBlockLength = len(d.buffer)
		} else {
			// align the current block with ChunkSize
			currentBlockLength = len(d.buffer) - len(d.buffer)%ChunkSize
		}

		opInfo := cf.CF_OPERATION_INFO{}
		opInfo.StructSize = uint32(unsafe.Sizeof(opInfo))
		opInfo.Type = cf.CF_OPERATION_TYPE_TRANSFER_DATA
		opInfo.ConnectionKey = d.context.ConnectionKey
		opInfo.TransferKey = d.context.TransferKey

		opParams := cf.CF_OPERATION_PARAMETERS{}
		opParams.ParamSize = sizeOfOpParamTransferData
		transfer := transferDataArm(&opParams)
		transfer.CompletionStatus = STATUS_SUCCESS
		if currentBlockLength > 0 {
			transfer.Buffer = unsafe.Pointer(&d.buffer[0])
		}
		transfer.Offset = d.offset
		transfer.Length = int64(currentBlockLength)

		cfExecuteErr := cf.CfExecute(&opInfo, &opParams)
		// the buffer is handed to the API as a raw pointer
		runtime.KeepAlive(d.buffer)
		if cfExecuteErr != nil {
			lcCfApiHydrationDevice.Critical("Couldn't send transfer info %s : %v",
				d.context, cfExecuteErr)
			d.errorString = cfExecuteErr.Error()
			return 0, cfExecuteErr
		}

		trailing := len(d.buffer) - currentBlockLength
		if trailing > 0 {
			// move the memory to the front
			copy(d.buffer, d.buffer[currentBlockLength:])
		}
		// this won't reduce the allocated size
		d.buffer = d.buffer[:trailing]

		d.offset += int64(currentBlockLength)
		// refresh Windows Copy Dialog progress
		progressTotal := d.totalSize
		progressCompleted := d.offset

		if err := cf.CfReportProviderProgress(d.context.ConnectionKey, d.context.TransferKey,
			progressTotal, progressCompleted); err != nil {
			lcCfApiHydrationDevice.Critical("Couldn't report provider progress %s : %v", d.context, err)
		}
	}
	return length, nil
}

// Close is HydrationDevice::close().
func (d *HydrationDevice) Close() error {
	if len(d.buffer) != 0 {
		// upstream asserts the buffer is empty here
		lcCfApiHydrationDevice.Warning("closing the hydration device with %d bytes still buffered", len(d.buffer))
	}
	return nil
}

// ErrorString is QIODevice::errorString().
func (d *HydrationDevice) ErrorString() string { return d.errorString }
