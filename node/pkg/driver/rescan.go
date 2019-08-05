package driver

import (
	"fmt"
	"k8s.io/klog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
    "sync"
	"io/ioutil"    
)

//go:generate mockgen -destination=../../mocks/mock_rescan_utils.go -package=mocks github.com/ibm/ibm-block-csi-driver/node/pkg/driver RescanUtilsInterface

type RescanUtilsInterface interface {
	RescanSpecificLun(Lun int, array_iqn string) error
	GetMpathDevice(volumeId string, lunId int, array_iqn string) (string, error)
	FlushMultipathDevice(mpathDevice string) error
	RemoveIscsiDevice(sysDevices []string) error
	GetIscsiSessionHostsForArrayIQN(array_iqn string) ([]int, error)	
}

type RescanUtilsIscsi struct {
	nodeUtils NodeUtilsInterface
	executor  ExecutorInterface
	mutexMultipathF *sync.Mutex
}

type NewRescanUtilsFunction func(connectivityType string, nodeUtils NodeUtilsInterface, executor ExecutorInterface) (RescanUtilsInterface, error)

func NewRescanUtils(connectivityType string, nodeUtils NodeUtilsInterface, executor ExecutorInterface) (RescanUtilsInterface, error) {
	klog.V(5).Infof("NewRescanUtils was called with connectivity type: %v", connectivityType)
	switch connectivityType {
	case "iscsi":
		return &RescanUtilsIscsi{nodeUtils: nodeUtils, executor: executor, mutexMultipathF: &sync.Mutex{}}, nil
	default:
		return nil, fmt.Errorf(ErrorUnsupportedConnectivityType, connectivityType)
	}
}

func (r RescanUtilsIscsi) RescanSpecificLun(lunId int, array_iqn string) error {
	klog.V(5).Infof("Starging Rescan specific lun, on lun : {%v}, with array iqn : {%v}", lunId, array_iqn)
	sessionHosts, err := r.GetIscsiSessionHostsForArrayIQN(array_iqn)
	if err != nil {
		return err
	}

	for _, hostNumber := range sessionHosts {

		filename := fmt.Sprintf("/sys/class/scsi_host/host%d/scan", hostNumber)
		f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0200)
		if err != nil {
			klog.Errorf("could not open filename : {%v}. err : {%v}", filename, err)
			return err
		}

		defer f.Close()

		scanCmd := fmt.Sprintf("0 0 %d", lunId)
		klog.V(5).Infof("Rescan host device : echo %s > %s", scanCmd, filename)
		if written, err := f.WriteString(scanCmd); err != nil {
			klog.Errorf("could not write to file :{%v}, error : {%v}", filename, err)
			return err
		} else if written == 0 {
			klog.Errorf("nothing was written to file : {%v}", filename)
			return fmt.Errorf(ErrorNothingWasWrittenToScanFile, filename)
		}

	}

	klog.V(5).Infof("finsihed rescan lun on lun id : {%v}, with array iqn : {%v}", lunId, array_iqn)
	return nil

}

func (r RescanUtilsIscsi) GetMpathDevice(volumeId string, lunId int, array_iqn string) (string, error) {
	/*
	   Description: 
		   1. Find all the files "/dev/disk/by-path/ip-<ARRAY-IP>-iscsi-<IQN storage>-lun-<LUN-ID> -> ../../sd<X>
 	          Note: <ARRAY-IP> Instead of setting here the IP we just search for * on that.
 	       2. Get the sd<X> devices.
 	       3. Search '/sys/block/dm-*\/slaves/*' and get the <DM device name>. For example dm-3 below:
 	          /sys/block/dm-3/slaves/sdb -> ../../../../platform/host41/session9/target41:0:0/41:0:0:13/block/sdb

	   Return Value: "dm-X" of the volumeID by using the LunID and the arrayIqn.
	*/
	var devicePaths []string

	devicePath := strings.Join([]string{"/dev/disk/by-path/ip*", "iscsi", array_iqn, "lun", strconv.Itoa(lunId)}, "-")
	klog.V(4).Infof("device path is : {%v}", devicePath)

	devicePaths, exists, err := waitForPathToExist(devicePath, 5, 1)
	if !exists {
		klog.V(4).Infof("return error because file was not found")
		return "", fmt.Errorf("could not find path")
	}
	if err != nil {
		klog.V(4).Infof("founr error : %v ", err.Error())
		return "", err
	}

	if len(devicePaths) < 1 {
		return "", fmt.Errorf("failed to find device path: %s", devicePath)
	}

	devicePathTosysfs := make(map[string]bool)
	// Looping over the physical devices of the volume - /dev/sdX (multiple since its with multipathing)
	for _, path := range devicePaths {
		if path != "" {
			if mappedDevicePath, err := getMultipathDisk(path); mappedDevicePath != "" {				
				devicePathTosysfs[mappedDevicePath] = true
				if err != nil {
					return "", err
				}
			}
		}
	}

	var mps string
	for key := range devicePathTosysfs{
		mps += ", " + key
	}

	klog.V(4).Infof("Found multipath devices: %s", mps)
	if len(devicePathTosysfs) > 1 {
		return "", &MultipleDmDevicesError{volumeId, lunId, array_iqn, devicePathTosysfs}
	} else if len(devicePathTosysfs) == 0 {
		return "", &MultipleDeviceNotFoundForLunError{volumeId, lunId, array_iqn}
	}
	var md string
	for md=range devicePathTosysfs{ break }  // because its the single value in the map, so just take the first
	return md, nil
}


func waitForPathToExist(devicePath string, maxRetries int, intervalSeconds int) ([]string, bool, error) {

	var err error
	for i := 0; i < maxRetries; i++ {
		err = nil
		fpaths, _ := filepath.Glob(devicePath)
		klog.V(4).Infof("fpaths : {%v}", fpaths)

		if fpaths == nil {
			err = os.ErrNotExist
		} else {
			return fpaths, true, nil
		}

		time.Sleep(time.Second * time.Duration(intervalSeconds))
	}
	return nil, false, err
}

func getMultipathDisk(path string) (string, error) {
	/*
		Description:
			Find the dm device based on path /dev/disk/by-path/TARGET-iscsi-iqn:<LUNID> -> ../../sdX
			By loop up on the /sys/block/dm-*\/slaves/sd<X> and return the <dm-*>
			
		Return Value: 
			dm-<X>
	*/
	
	// Follow link to destination directory
	klog.V(5).Infof("Getting multipaht disk")
	devicePath, err := os.Readlink(path)
	if err != nil {
		klog.V(4).Infof("Failed reading link for multipath disk: %s. error: {%s}\n", path, err.Error())
		return "", err
	}
	// Get only the physical device from /dev/disk/by-path/TARGET-iscsi-iqn:<LUNID> -> ../../sdb
	sdevice := filepath.Base(devicePath)
	// If destination directory is already identified as a multipath device,
	// just return its path
	if strings.HasPrefix(sdevice, "dm-") {
		klog.V(4).Infof("Already found multipath device: %s", sdevice)
		return path, nil
	}
	// Fallback to iterating through all the entries under /sys/block/dm-* and
	// check to see if any have an entry under /sys/block/dm-*/slaves matching
	// the device the symlink was pointing at
	dmPaths, err := filepath.Glob("/sys/block/dm-*")
	// TODO improve looping by just filepath.Glob("/sys/block/dm-*/slaves/" + sdevice) and then no loops needed below, since it will just find the device directly.

	if err != nil {
		klog.V(4).Infof("Glob error: %s", err)
		return "", err
	}
	for _, dmPath := range dmPaths {
		sdevices, err := filepath.Glob(filepath.Join(dmPath, "slaves", "*"))
		if err != nil {
			klog.V(4).Infof("Glob error: %s", err)
		}
		for _, spath := range sdevices {
			s := filepath.Base(spath)
			if sdevice == s {
				// We've found a matching entry, return the path for the
				// dm-* device it was found under
				// for Example, return /dev/dm-3
				//   ls -l  /sys/block/dm-*/slaves/*
				//    /sys/block/dm-3/slaves/sdb -> ../../../../platform/host41/session9/target41:0:0/41:0:0:13/block/sdb

				p := filepath.Join("/dev", filepath.Base(dmPath))
				klog.V(4).Infof("Found matching multipath device: %s under dm-* device path %s", sdevice, dmPath)
				return p, nil
			}
		}
	}
	
	err = &MultipleDeviceNotFoundError{path, devicePath}
	klog.Errorf(err.Error())
	return "", err
}

func (r RescanUtilsIscsi) FlushMultipathDevice(mpathDevice string) error {
	// mpathdevice is dm-4 for example

	klog.V(5).Infof("flushing mpath device : {%v}", mpathDevice)

	fullDevice := "/dev/" + mpathDevice


	klog.V(5).Infof("Try to accure lock for running the command multipath -f {%v} (to avoid concurrent multipath commands)", mpathDevice)
	r.mutexMultipathF.Lock()
	klog.V(5).Infof("Accured lock for multipath -f command")
	_, err := r.executor.ExecuteWithTimeout(10*1000, "multipath", []string{"-f", fullDevice})
	r.mutexMultipathF.Unlock()
		
	if err != nil {
		if _, errOpen := os.Open(fullDevice); errOpen != nil {
			if os.IsNotExist(errOpen) {
				klog.V(5).Infof("Mpath device {%v} was deleted", fullDevice)
			} else {
				klog.Errorf("Error while opening file : {%v}. error: {%v}. Means the multipath -f {%v} did not succeed to delete the device.", fullDevice, errOpen.Error(), fullDevice)
				return errOpen
			}
		} else{
			klog.Errorf("multipath -f {%v} did not succeed to delete the device. err={%v}", fullDevice, err.Error())
			return err
		}
	}

	klog.V(5).Infof("finshed flushing mpath device : {%v}", mpathDevice)
	return nil

}

func (r RescanUtilsIscsi) RemoveIscsiDevice(sysDevices []string) error {
	// sysDevices  = sdb, sda,...
	klog.V(5).Infof("Removing iscsi device : {%v}", sysDevices)

	var (
		f   *os.File
		err error
	)

	for _, deviceName := range sysDevices {
		if deviceName == "" {
			continue
		}
		klog.V(5).Infof("opening device for device name : {%v}", deviceName)
		

		filename := fmt.Sprintf("/sys/block/%s/device/delete", deviceName)
		if f, err = os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0200); err != nil {
			if os.IsNotExist(err) {
				klog.V(5).Infof("Block device {%v} was not found on the system, so skip deleting it", deviceName)
			} else {
				klog.Errorf("errow while opening file : {%v}. error: {%v}", filename, err.Error())
				return err
			}
		}

		defer f.Close()

		if _, err := f.WriteString("1"); err != nil {
			klog.Errorf("eror while writing to file : {%v}. error: {%v}", filename, err.Error())
			return err // TODO: maybe we need to just swallow the error and continnue??
		}
	}
	klog.V(5).Infof("finshed removing iscsi device : {%v}", sysDevices)
	return nil
}

func (r RescanUtilsIscsi) GetIscsiSessionHostsForArrayIQN(array_iqn string) ([]int, error) {
	/*
		Description:
			This function find all the hosts IDs under which has targetname that equal to the array_iqn.  
			/sys/class/iscsi_host/host<IDs>/device/session*\/iscsi_session/session*\/targetname"
			So the function goes over all the above hosts and return back only the host numbers as a list.
	*/
	
	sysPath := "/sys/class/iscsi_host/"
	var sessionHosts []int
	if hostDirs, err := ioutil.ReadDir(sysPath); err != nil {
		klog.Errorf("cannot read sys dir : {%v}. error : {%v}", sysPath, err)
		return sessionHosts, err
	} else {
		klog.V(5).Infof("host dirs : {%v}", hostDirs)
		for _, hostDir := range hostDirs {
			// get the host session number : "host34"
			hostName := hostDir.Name()
			hostNumber := -1
			if !strings.HasPrefix(hostName, "host") {
				continue
			} else {
				hostNumber, err = strconv.Atoi(strings.TrimPrefix(hostName, "host"))
				if err != nil {
					klog.V(4).Infof("cannot get host id from host : {%v}", hostName)
					continue
				}
			}

			targetPath := sysPath + hostName + "/device/session*/iscsi_session/session*/targetname"

			//devicePath + sessionName + "/iscsi_session/" + sessionName + "/targetname"
			matches, err := filepath.Glob(targetPath)
			if err != nil {
				klog.Errorf("error while finding targetPath : {%v}. err : {%v}", targetPath, err)
				return sessionHosts, err
			}

			klog.V(5).Infof("matches were found : {%v}", matches)

			//TODO: can there be more then 1 session??
			//sessionNumber, err :=  strconv.Atoi(strings.TrimPrefix(matches[0], "session"))

			if len(matches) == 0 {
				klog.V(4).Infof("could not find targe name for host : {%v}, path : {%v}", hostName, targetPath)
				continue
			}

			targetNamePath := matches[0]
			targetName, err := ioutil.ReadFile(targetNamePath)
			if err != nil {
				klog.V(4).Infof("could not read target name from file : {%v}, error : {%v}", targetNamePath, err)
				continue
			}

			klog.V(5).Infof("target name found : {%v}", string(targetName))

			if strings.TrimSpace(string(targetName)) == array_iqn {
				sessionHosts = append(sessionHosts, hostNumber)
				klog.V(5).Infof("host nunber appended : {%v}. sessionhosts is : {%v}", hostNumber, sessionHosts)
			}
		}



		if len(sessionHosts) == 0 {
			genericTargetPath := sysPath + "host*" + "/device/session*/iscsi_session/session*/targetname"
			return []int{}, &ConnectivityIscsiStorageTargetNotFoundError{StorageTargetName:array_iqn, DirectoryPath:genericTargetPath}
		}
		return sessionHosts, nil
	}
}

/*



// flushDevice flushes any outstanding I/O to all paths to a device.
func flushDevice(deviceInfo *ScsiDeviceInfo) {

	log.Debug(">>>> osutils.flushDevice")
	defer log.Debug("<<<< osutils.flushDevice")

	for _, device := range deviceInfo.Devices {
		_, err := execCommandWithTimeout("blockdev", 5, "--flushbufs", "/dev/"+device)
		if err != nil {
			// nothing to do if it generates an error but log it
			log.WithFields(log.Fields{
				"device": device,
				"error":  err,
			}).Warning("Error encountered in blockdev --flushbufs command.")
		}
	}
}


*/
