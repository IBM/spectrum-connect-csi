/**
 * Copyright 2019 IBM Corp.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package driver_test

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/mock/gomock"
	"github.com/ibm/ibm-block-csi-driver/node/mocks"
	"github.com/ibm/ibm-block-csi-driver/node/pkg/driver/device_connectivity"

	"github.com/ibm/ibm-block-csi-driver/node/pkg/driver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	PublishContextParamLun          string = "PUBLISH_CONTEXT_LUN" // TODO for some reason I coun't take it from config.yaml
	PublishContextParamConnectivity string = "PUBLISH_CONTEXT_CONNECTIVITY"
	PublishContextParamArrayIqn     string = "PUBLISH_CONTEXT_ARRAY_IQN"
)

func newTestNodeService(nodeUtils driver.NodeUtilsInterface, osDevCon device_connectivity.OsDeviceConnectivityInterface, nodeMounter driver.NodeMounter) driver.NodeService {
	return driver.NodeService{
		Hostname:                   "test-host",
		ConfigYaml:                 driver.ConfigFile{},
		VolumeIdLocksMap:           driver.NewSyncLock(),
		NodeUtils:                  nodeUtils,
		Mounter:                    nodeMounter,
		OsDeviceConnectivityHelper: osDevCon,
	}
}

func newTestNodeServiceStaging(nodeUtils driver.NodeUtilsInterface, osDevCon device_connectivity.OsDeviceConnectivityInterface, nodeMounter driver.NodeMounter) driver.NodeService {
	osDeviceConnectivityMapping := map[string]device_connectivity.OsDeviceConnectivityInterface{
		device_connectivity.ConnectionTypeISCSI: osDevCon,
		device_connectivity.ConnectionTypeFC:    osDevCon,
	}

	return driver.NodeService{
		Mounter:                     nodeMounter,
		Hostname:                    "test-host",
		ConfigYaml:                  driver.ConfigFile{},
		VolumeIdLocksMap:            driver.NewSyncLock(),
		NodeUtils:                   nodeUtils,
		OsDeviceConnectivityMapping: osDeviceConnectivityMapping,
		OsDeviceConnectivityHelper:  osDevCon,
	}
}

func TestNodeStageVolume(t *testing.T) {
	dummyError := errors.New("Dummy error")
	conType := device_connectivity.ConnectionTypeISCSI
	volId := "vol-test"
	lun := 10
	mpathDeviceName := "dm-2"
	mpathDevice := "/dev/" + mpathDeviceName
	fsType := "ext4"
	ipsByArrayInitiator := map[string][]string{"iqn.1994-05.com.redhat:686358c930fe": {"1.2.3.4", "[::1]"}}
	arrayInitiators := []string{"iqn.1994-05.com.redhat:686358c930fe"}
	stagingPath := "/test/path"
	stagingPathWithHostPrefix := GetPodPath(stagingPath)
	var mountOptions []string

	stdVolCap := &csi.VolumeCapability{
		AccessType: &csi.VolumeCapability_Mount{
			Mount: &csi.VolumeCapability_MountVolume{FsType: fsType},
		},
		AccessMode: &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		},
	}
	publishContext := map[string]string{
		PublishContextParamLun:                "1",
		PublishContextParamConnectivity:       device_connectivity.ConnectionTypeISCSI,
		PublishContextParamArrayIqn:           "iqn.1994-05.com.redhat:686358c930fe",
		"iqn.1994-05.com.redhat:686358c930fe": "1.2.3.4,[::1]",
	}
	stagingRequest := &csi.NodeStageVolumeRequest{
		PublishContext:    publishContext,
		StagingTargetPath: stagingPath,
		VolumeCapability:  stdVolCap,
		VolumeId:          volId,
	}

	testCases := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{
			name: "fail no VolumeId",
			testFunc: func(t *testing.T) {
				req := &csi.NodeStageVolumeRequest{
					PublishContext:    publishContext,
					StagingTargetPath: stagingPath,
					VolumeCapability:  stdVolCap,
				}
				node := newTestNodeService(nil, nil, nil)
				_, err := node.NodeStageVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail no StagingTargetPath",
			testFunc: func(t *testing.T) {
				req := &csi.NodeStageVolumeRequest{
					PublishContext:   publishContext,
					VolumeCapability: stdVolCap,
					VolumeId:         volId,
				}
				node := newTestNodeService(nil, nil, nil)
				_, err := node.NodeStageVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail no VolumeCapability",
			testFunc: func(t *testing.T) {
				req := &csi.NodeStageVolumeRequest{
					PublishContext:    publishContext,
					StagingTargetPath: stagingPath,
					VolumeId:          volId,
				}
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				node := newTestNodeService(mockNodeUtils, nil, nil)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(stagingPathWithHostPrefix).Return(true)

				_, err := node.NodeStageVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail invalid VolumeCapability",
			testFunc: func(t *testing.T) {
				req := &csi.NodeStageVolumeRequest{
					PublishContext:    publishContext,
					StagingTargetPath: stagingPath,
					VolumeCapability: &csi.VolumeCapability{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_UNKNOWN,
						},
					},
					VolumeId: volId,
				}
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				node := newTestNodeService(mockNodeUtils, nil, nil)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(stagingPathWithHostPrefix).Return(true)

				_, err := node.NodeStageVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail invalid arrayInitiators",
			testFunc: func(t *testing.T) {
				req := &csi.NodeStageVolumeRequest{
					PublishContext: map[string]string{
						PublishContextParamLun:          "1",
						PublishContextParamConnectivity: device_connectivity.ConnectionTypeISCSI,
						PublishContextParamArrayIqn:     "iqn.1994-05.com.redhat:686358c930fe",
					},
					StagingTargetPath: stagingPath,
					VolumeCapability: &csi.VolumeCapability{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
					VolumeId: volId,
				}
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				node := newTestNodeService(mockNodeUtils, nil, nil)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(stagingPathWithHostPrefix).Return(true)

				_, err := node.NodeStageVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail parse PublishContext",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				node := newTestNodeService(mockNodeUtils, nil, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(stagingPathWithHostPrefix).Return(true)
				mockNodeUtils.EXPECT().GetInfoFromPublishContext(stagingRequest.PublishContext, node.ConfigYaml).Return("", 0, nil, dummyError)

				_, err := node.NodeStageVolume(context.TODO(), stagingRequest)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail rescan devices",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceStaging(mockNodeUtils, mockOsDeviceCon, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(stagingPathWithHostPrefix).Return(true)
				mockNodeUtils.EXPECT().GetInfoFromPublishContext(stagingRequest.PublishContext, node.ConfigYaml).Return(conType, lun, ipsByArrayInitiator, nil).AnyTimes()
				mockNodeUtils.EXPECT().GetArrayInitiators(ipsByArrayInitiator).Return(arrayInitiators)
				mockOsDeviceCon.EXPECT().EnsureLogin(ipsByArrayInitiator)
				mockOsDeviceCon.EXPECT().RescanDevices(lun, arrayInitiators).Return(dummyError)

				_, err := node.NodeStageVolume(context.TODO(), stagingRequest)
				assertError(t, err, codes.Internal)
			},
		},
		{
			name: "fail get mpath device",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceStaging(mockNodeUtils, mockOsDeviceCon, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(stagingPathWithHostPrefix).Return(true)
				mockNodeUtils.EXPECT().GetInfoFromPublishContext(stagingRequest.PublishContext, node.ConfigYaml).Return(conType, lun, ipsByArrayInitiator, nil).AnyTimes()
				mockNodeUtils.EXPECT().GetArrayInitiators(ipsByArrayInitiator).Return(arrayInitiators)
				mockOsDeviceCon.EXPECT().EnsureLogin(ipsByArrayInitiator)
				mockOsDeviceCon.EXPECT().RescanDevices(lun, arrayInitiators).Return(nil)
				mockOsDeviceCon.EXPECT().GetMpathDevice(volId).Return("", dummyError)

				_, err := node.NodeStageVolume(context.TODO(), stagingRequest)
				assertError(t, err, codes.Internal)
			},
		},
		{
			name: "fail get disk format",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceStaging(mockNodeUtils, mockOsDeviceCon, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(stagingPathWithHostPrefix).Return(true)
				mockNodeUtils.EXPECT().GetInfoFromPublishContext(stagingRequest.PublishContext, node.ConfigYaml).Return(conType, lun, ipsByArrayInitiator, nil).AnyTimes()
				mockNodeUtils.EXPECT().GetArrayInitiators(ipsByArrayInitiator).Return(arrayInitiators)
				mockOsDeviceCon.EXPECT().EnsureLogin(ipsByArrayInitiator)
				mockOsDeviceCon.EXPECT().RescanDevices(lun, arrayInitiators).Return(nil)
				mockOsDeviceCon.EXPECT().GetMpathDevice(volId).Return(mpathDevice, nil)
				mockMounter.EXPECT().GetDiskFormat(mpathDevice).Return("", dummyError)

				_, err := node.NodeStageVolume(context.TODO(), stagingRequest)
				assertError(t, err, codes.Internal)
			},
		},
		{
			name: "success new filesystem",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceStaging(mockNodeUtils, mockOsDeviceCon, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(stagingPathWithHostPrefix).Return(true)
				mockNodeUtils.EXPECT().GetInfoFromPublishContext(stagingRequest.PublishContext, node.ConfigYaml).Return(conType, lun, ipsByArrayInitiator, nil).AnyTimes()
				mockNodeUtils.EXPECT().GetArrayInitiators(ipsByArrayInitiator).Return(arrayInitiators)
				mockOsDeviceCon.EXPECT().EnsureLogin(ipsByArrayInitiator)
				mockOsDeviceCon.EXPECT().RescanDevices(lun, arrayInitiators).Return(nil)
				mockOsDeviceCon.EXPECT().GetMpathDevice(volId).Return(mpathDevice, nil)
				mockMounter.EXPECT().GetDiskFormat(mpathDevice).Return("", nil)
				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsNotMountPoint(stagingPathWithHostPrefix).Return(true, nil)
				mockNodeUtils.EXPECT().FormatDevice(mpathDevice, fsType)
				mockMounter.EXPECT().FormatAndMount(mpathDevice, stagingPath, fsType, mountOptions)

				_, err := node.NodeStageVolume(context.TODO(), stagingRequest)
				if err != nil {
					t.Fatalf("Expect no error but got: %v", err)
				}
			},
		},
		{
			name: "success device already formatted",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceStaging(mockNodeUtils, mockOsDeviceCon, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(stagingPathWithHostPrefix).Return(true)
				mockNodeUtils.EXPECT().GetInfoFromPublishContext(stagingRequest.PublishContext, node.ConfigYaml).Return(conType, lun, ipsByArrayInitiator, nil).AnyTimes()
				mockNodeUtils.EXPECT().GetArrayInitiators(ipsByArrayInitiator).Return(arrayInitiators)
				mockOsDeviceCon.EXPECT().EnsureLogin(ipsByArrayInitiator)
				mockOsDeviceCon.EXPECT().RescanDevices(lun, arrayInitiators).Return(nil)
				mockOsDeviceCon.EXPECT().GetMpathDevice(volId).Return(mpathDevice, nil)
				mockMounter.EXPECT().GetDiskFormat(mpathDevice).Return(fsType, nil)
				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsNotMountPoint(stagingPathWithHostPrefix).Return(true, nil)
				mockMounter.EXPECT().FormatAndMount(mpathDevice, stagingPath, fsType, mountOptions)

				_, err := node.NodeStageVolume(context.TODO(), stagingRequest)
				if err != nil {
					t.Fatalf("Expect no error but got: %v", err)
				}
			},
		},
		{
			name: "success idempotent",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceStaging(mockNodeUtils, mockOsDeviceCon, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(stagingPathWithHostPrefix).Return(true)
				mockNodeUtils.EXPECT().GetInfoFromPublishContext(stagingRequest.PublishContext, node.ConfigYaml).Return(conType, lun, ipsByArrayInitiator, nil).AnyTimes()
				mockNodeUtils.EXPECT().GetArrayInitiators(ipsByArrayInitiator).Return(arrayInitiators)
				mockOsDeviceCon.EXPECT().EnsureLogin(ipsByArrayInitiator)
				mockOsDeviceCon.EXPECT().RescanDevices(lun, arrayInitiators).Return(nil)
				mockOsDeviceCon.EXPECT().GetMpathDevice(volId).Return(mpathDevice, nil)
				mockMounter.EXPECT().GetDiskFormat(mpathDevice).Return(fsType, nil)
				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsNotMountPoint(stagingPathWithHostPrefix).Return(false, nil)
				mockNodeUtils.EXPECT().IsDirectory(stagingPathWithHostPrefix).Return(true)

				_, err := node.NodeStageVolume(context.TODO(), stagingRequest)
				if err != nil {
					t.Fatalf("Expect no error but got: %v", err)
				}
			},
		},
		{
			name: "fail existing fsType different from requested",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceStaging(mockNodeUtils, mockOsDeviceCon, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(stagingPathWithHostPrefix).Return(true)
				mockNodeUtils.EXPECT().GetInfoFromPublishContext(stagingRequest.PublishContext, node.ConfigYaml).Return(conType, lun, ipsByArrayInitiator, nil).AnyTimes()
				mockNodeUtils.EXPECT().GetArrayInitiators(ipsByArrayInitiator).Return(arrayInitiators)
				mockOsDeviceCon.EXPECT().EnsureLogin(ipsByArrayInitiator)
				mockOsDeviceCon.EXPECT().RescanDevices(lun, arrayInitiators).Return(nil)
				mockOsDeviceCon.EXPECT().GetMpathDevice(volId).Return(mpathDevice, nil)
				mockMounter.EXPECT().GetDiskFormat(mpathDevice).Return("different-fsType", nil)

				_, err := node.NodeStageVolume(context.TODO(), stagingRequest)
				assertError(t, err, codes.AlreadyExists)
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, tc.testFunc)
	}
}

func TestNodeUnstageVolume(t *testing.T) {
	volId := "vol-test"
	dummyError := errors.New("Dummy error")
	dmNotFoundError := &device_connectivity.MultipathDeviceNotFoundForVolumeError{VolumeId: volId}
	mpathDeviceName := "dm-2"
	rawSysDevices := "/dev/d1,/dev/d2"
	sysDevices := strings.Split(rawSysDevices, ",")
	stagingPath := "/test/path"
	stageInfoPath := path.Join(stagingPath, driver.StageInfoFilename)
	stagingPathWithHostPrefix := GetPodPath(stagingPath)

	unstageRequest := &csi.NodeUnstageVolumeRequest{
		VolumeId:          volId,
		StagingTargetPath: stagingPath,
	}

	testCases := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{
			name: "fail no VolumeId",
			testFunc: func(t *testing.T) {
				node := newTestNodeService(nil, nil, nil)
				req := &csi.NodeUnstageVolumeRequest{
					StagingTargetPath: stagingPath,
				}
				_, err := node.NodeUnstageVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail no StagingTargetPath",
			testFunc: func(t *testing.T) {
				node := newTestNodeService(nil, nil, nil)
				req := &csi.NodeUnstageVolumeRequest{
					VolumeId: volId,
				}
				_, err := node.NodeUnstageVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail discovering multipath device",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				node := newTestNodeServiceStaging(mockNodeUtils, mockOsDeviceCon, nil)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsNotMountPoint(stagingPathWithHostPrefix).Return(true, nil)
				mockOsDeviceCon.EXPECT().GetMpathDevice(volId).Return("", dummyError)

				_, err := node.NodeUnstageVolume(context.TODO(), unstageRequest)
				assertError(t, err, codes.Internal)
			},
		},
		{
			name: "fail flush multipath",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceStaging(mockNodeUtils, mockOsDeviceCon, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsNotMountPoint(stagingPathWithHostPrefix).Return(true, nil)
				mockOsDeviceCon.EXPECT().GetMpathDevice(volId).Return(mpathDeviceName, nil)
				mockNodeUtils.EXPECT().GetSysDevicesFromMpath(mpathDeviceName).Return(rawSysDevices, nil)
				mockOsDeviceCon.EXPECT().FlushMultipathDevice(mpathDeviceName).Return(dummyError)

				_, err := node.NodeUnstageVolume(context.TODO(), unstageRequest)
				assertError(t, err, codes.Internal)
			},
		},
		{
			name: "success idempotent",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceStaging(mockNodeUtils, mockOsDeviceCon, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsNotMountPoint(stagingPathWithHostPrefix).Return(true, nil)
				mockOsDeviceCon.EXPECT().GetMpathDevice(volId).Return("", dmNotFoundError)

				_, err := node.NodeUnstageVolume(context.TODO(), unstageRequest)
				if err != nil {
					t.Fatalf("Expect no error but got: %v", err)
				}
			},
		},
		{
			name: "success normal",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceStaging(mockNodeUtils, mockOsDeviceCon, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(stagingPath).Return(stagingPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsNotMountPoint(stagingPathWithHostPrefix).Return(false, nil)
				mockMounter.EXPECT().Unmount(stagingPath).Return(nil)
				mockOsDeviceCon.EXPECT().GetMpathDevice(volId).Return(mpathDeviceName, nil)
				mockNodeUtils.EXPECT().GetSysDevicesFromMpath(mpathDeviceName).Return(rawSysDevices, nil)
				mockOsDeviceCon.EXPECT().FlushMultipathDevice(mpathDeviceName).Return(nil)
				mockOsDeviceCon.EXPECT().RemovePhysicalDevice(sysDevices).Return(nil)
				mockNodeUtils.EXPECT().StageInfoFileIsExist(stageInfoPath).Return(true)
				mockNodeUtils.EXPECT().ClearStageInfoFile(stageInfoPath).Return(nil)

				_, err := node.NodeUnstageVolume(context.TODO(), unstageRequest)
				if err != nil {
					t.Fatalf("Expect no error but got: %v", err)
				}
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, tc.testFunc)
	}
}

func TestNodePublishVolume(t *testing.T) {
	volumeId := "vol-test"
	fsTypeXfs := "ext4"
	targetPath := "/test/path"
	targetPathWithHostPrefix := GetPodPath(targetPath)
	stagingTargetPath := path.Join("/test/staging", driver.StageInfoFilename)
	deviceName := "fakedev"
	mpathDevice := filepath.Join(device_connectivity.DevPath, deviceName)
	accessMode := &csi.VolumeCapability_AccessMode{
		Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
	}
	fsVolCap := &csi.VolumeCapability{
		AccessType: &csi.VolumeCapability_Mount{
			Mount: &csi.VolumeCapability_MountVolume{FsType: fsTypeXfs},
		},
		AccessMode: accessMode,
	}
	rawBlockVolumeCap := &csi.VolumeCapability{
		AccessType: &csi.VolumeCapability_Block{
			Block: &csi.VolumeCapability_BlockVolume{},
		},
		AccessMode: accessMode,
	}

	testCases := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{
			name: "fail no VolumeId",
			testFunc: func(t *testing.T) {
				node := newTestNodeService(nil, nil, nil)
				req := &csi.NodePublishVolumeRequest{
					PublishContext:    map[string]string{},
					StagingTargetPath: stagingTargetPath,
					TargetPath:        targetPath,
					VolumeCapability:  fsVolCap,
				}

				_, err := node.NodePublishVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail no StagingTargetPath",
			testFunc: func(t *testing.T) {
				node := newTestNodeService(nil, nil, nil)
				req := &csi.NodePublishVolumeRequest{
					PublishContext:   map[string]string{},
					TargetPath:       targetPath,
					VolumeCapability: fsVolCap,
					VolumeId:         volumeId,
				}

				_, err := node.NodePublishVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail no TargetPath",
			testFunc: func(t *testing.T) {
				node := newTestNodeService(nil, nil, nil)
				req := &csi.NodePublishVolumeRequest{
					PublishContext:    map[string]string{},
					StagingTargetPath: stagingTargetPath,
					VolumeCapability:  fsVolCap,
					VolumeId:          volumeId,
				}

				_, err := node.NodePublishVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail no VolumeCapability",
			testFunc: func(t *testing.T) {
				node := newTestNodeService(nil, nil, nil)
				req := &csi.NodePublishVolumeRequest{
					PublishContext:    map[string]string{},
					StagingTargetPath: stagingTargetPath,
					TargetPath:        targetPath,
					VolumeId:          volumeId,
				}

				_, err := node.NodePublishVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail invalid VolumeCapability",
			testFunc: func(t *testing.T) {
				node := newTestNodeService(nil, nil, nil)
				req := &csi.NodePublishVolumeRequest{
					PublishContext:    map[string]string{},
					StagingTargetPath: stagingTargetPath,
					TargetPath:        targetPath,
					VolumeCapability: &csi.VolumeCapability{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_UNKNOWN,
						},
					},
					VolumeId: volumeId,
				}

				_, err := node.NodePublishVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail AlreadyExists",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				node := newTestNodeService(mockNodeUtils, nil, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(targetPath).Return(targetPathWithHostPrefix).AnyTimes()
				mockNodeUtils.EXPECT().IsPathExists(targetPathWithHostPrefix).Return(true)
				mockNodeUtils.EXPECT().IsNotMountPoint(targetPathWithHostPrefix).Return(false, nil)
				mockNodeUtils.EXPECT().IsDirectory(targetPathWithHostPrefix).Return(false)

				req := &csi.NodePublishVolumeRequest{
					PublishContext:    map[string]string{},
					StagingTargetPath: stagingTargetPath,
					TargetPath:        targetPath,
					VolumeCapability:  fsVolCap,
					VolumeId:          volumeId,
				}

				_, err := node.NodePublishVolume(context.TODO(), req)
				assertError(t, err, codes.AlreadyExists)
			},
		},
		{
			name: "success with filesystem volume",
			testFunc: func(t *testing.T) {
				mountOptions := []string{"bind"}
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				node := newTestNodeService(mockNodeUtils, nil, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(targetPath).Return(targetPathWithHostPrefix).AnyTimes()
				mockNodeUtils.EXPECT().IsPathExists(targetPathWithHostPrefix).Return(false)
				mockNodeUtils.EXPECT().MakeDir(targetPathWithHostPrefix).Return(nil)
				mockMounter.EXPECT().Mount(stagingTargetPath, targetPath, fsTypeXfs, mountOptions)

				req := &csi.NodePublishVolumeRequest{
					PublishContext:    map[string]string{},
					StagingTargetPath: stagingTargetPath,
					TargetPath:        targetPath,
					VolumeCapability:  fsVolCap,
					VolumeId:          volumeId,
				}

				_, err := node.NodePublishVolume(context.TODO(), req)
				if err != nil {
					t.Fatalf("Expect no error but got: %v", err)
				}
			},
		},
		{
			name: "success idempotent with filesystem volume",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				node := newTestNodeService(mockNodeUtils, nil, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(targetPath).Return(targetPathWithHostPrefix).AnyTimes()
				mockNodeUtils.EXPECT().IsPathExists(targetPathWithHostPrefix).Return(true)
				mockNodeUtils.EXPECT().IsNotMountPoint(targetPathWithHostPrefix).Return(false, nil)
				mockNodeUtils.EXPECT().IsDirectory(targetPathWithHostPrefix).Return(true)

				req := &csi.NodePublishVolumeRequest{
					PublishContext:    map[string]string{},
					StagingTargetPath: stagingTargetPath,
					TargetPath:        targetPath,
					VolumeCapability:  fsVolCap,
					VolumeId:          volumeId,
				}

				_, err := node.NodePublishVolume(context.TODO(), req)
				if err != nil {
					t.Fatalf("Expect no error but got: %v", err)
				}
			},
		},
		{
			name: "success with raw block volume",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				node := newTestNodeService(mockNodeUtils, mockOsDeviceCon, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(targetPath).Return(targetPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(targetPathWithHostPrefix).Return(false)
				mockNodeUtils.EXPECT().MakeFile(gomock.Eq(targetPathWithHostPrefix)).Return(nil)
				mockOsDeviceCon.EXPECT().GetMpathDevice(volumeId).Return(mpathDevice, nil)
				mockMounter.EXPECT().Mount(mpathDevice, targetPath, "", []string{"bind"})

				req := &csi.NodePublishVolumeRequest{
					PublishContext:    map[string]string{},
					StagingTargetPath: stagingTargetPath,
					TargetPath:        targetPath,
					VolumeCapability:  rawBlockVolumeCap,
					VolumeId:          "vol-test",
				}

				_, err := node.NodePublishVolume(context.TODO(), req)
				if err != nil {
					t.Fatalf("Expect no error but got: %v", err)
				}
			},
		},
		{
			name: "success with raw block volume with mount file exits",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				mockOsDeviceCon := mocks.NewMockOsDeviceConnectivityInterface(mockCtl)
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				node := newTestNodeService(mockNodeUtils, mockOsDeviceCon, mockMounter)

				mockNodeUtils.EXPECT().GetPodPath(targetPath).Return(targetPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(targetPathWithHostPrefix).Return(true)
				mockNodeUtils.EXPECT().IsNotMountPoint(targetPathWithHostPrefix).Return(true, nil)
				mockOsDeviceCon.EXPECT().GetMpathDevice(volumeId).Return(mpathDevice, nil)
				mockMounter.EXPECT().Mount(mpathDevice, targetPath, "", []string{"bind"})

				req := &csi.NodePublishVolumeRequest{
					PublishContext:    map[string]string{},
					StagingTargetPath: stagingTargetPath,
					TargetPath:        targetPath,
					VolumeCapability:  rawBlockVolumeCap,
					VolumeId:          "vol-test",
				}

				_, err := node.NodePublishVolume(context.TODO(), req)
				if err != nil {
					t.Fatalf("Expect no error but got: %v", err)
				}
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, tc.testFunc)
	}
}

func TestNodeUnpublishVolume(t *testing.T) {
	targetPath := "/test/path"
	targetPathWithHostPrefix := GetPodPath(targetPath)

	testCases := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{
			name: "fail no VolumeId",
			testFunc: func(t *testing.T) {
				node := newTestNodeService(nil, nil, nil)

				req := &csi.NodeUnpublishVolumeRequest{
					TargetPath: targetPath,
				}
				_, err := node.NodeUnpublishVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail no TargetPath",
			testFunc: func(t *testing.T) {
				node := newTestNodeService(nil, nil, nil)

				req := &csi.NodeUnpublishVolumeRequest{
					VolumeId: "vol-test",
				}
				_, err := node.NodeUnpublishVolume(context.TODO(), req)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "success normal",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				node := newTestNodeService(mockNodeUtils, nil, mockMounter)

				req := &csi.NodeUnpublishVolumeRequest{
					TargetPath: targetPath,
					VolumeId:   "vol-test",
				}
				mockNodeUtils.EXPECT().GetPodPath(targetPath).Return(targetPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(targetPathWithHostPrefix).Return(true)
				mockNodeUtils.EXPECT().IsNotMountPoint(targetPathWithHostPrefix).Return(false, nil)
				mockMounter.EXPECT().Unmount(targetPath).Return(nil)
				mockNodeUtils.EXPECT().RemoveFileOrDirectory(targetPathWithHostPrefix)
				_, err := node.NodeUnpublishVolume(context.TODO(), req)
				if err != nil {
					t.Fatalf("Expect no error but got: %v", err)
				}
			},
		},
		{
			name: "success idempotent",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				node := newTestNodeService(mockNodeUtils, nil, mockMounter)

				req := &csi.NodeUnpublishVolumeRequest{
					TargetPath: targetPath,
					VolumeId:   "vol-test",
				}
				mockNodeUtils.EXPECT().GetPodPath(targetPath).Return(targetPathWithHostPrefix)
				mockNodeUtils.EXPECT().IsPathExists(targetPathWithHostPrefix).Return(false)
				_, err := node.NodeUnpublishVolume(context.TODO(), req)
				if err != nil {
					t.Fatalf("Expect no error but got: %v", err)
				}
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, tc.testFunc)
	}
}

func TestNodeGetVolumeStats(t *testing.T) {

	req := &csi.NodeGetVolumeStatsRequest{}

	d := newTestNodeService(nil, nil, nil)

	expErrCode := codes.Unimplemented

	_, err := d.NodeGetVolumeStats(context.TODO(), req)
	if err == nil {
		t.Fatalf("Expected error code %d, got nil", expErrCode)
	}
	srvErr, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Could not get error status code from error: %v", srvErr)
	}
	if srvErr.Code() != expErrCode {
		t.Fatalf("Expected error code %d, got %d message %s", expErrCode, srvErr.Code(), srvErr.Message())
	}
}

func newTestNodeServiceExpand(nodeUtils driver.NodeUtilsInterface, osDevConHelper device_connectivity.OsDeviceConnectivityHelperScsiGenericInterface, nodeMounter driver.NodeMounter) driver.NodeService {
	return driver.NodeService{
		Hostname:                    "test-host",
		ConfigYaml:                  driver.ConfigFile{},
		VolumeIdLocksMap:            driver.NewSyncLock(),
		NodeUtils:                   nodeUtils,
		OsDeviceConnectivityMapping: map[string]device_connectivity.OsDeviceConnectivityInterface{},
		OsDeviceConnectivityHelper:  osDevConHelper,
		Mounter:                     nodeMounter,
	}
}

func TestNodeExpandVolume(t *testing.T) {
	d := newTestNodeService(nil, nil, nil)
	volId := "someStorageType:vol-test"
	volumePath := "/test/path"
	stagingTargetPath := "/staging/test/path"
	expandRequest := &csi.NodeExpandVolumeRequest{
		VolumeId:          volId,
		VolumePath:        volumePath,
		StagingTargetPath: stagingTargetPath,
	}
	mpathDeviceName := "dm-2"
	rawSysDevices := "/dev/d1,/dev/d2"
	sysDevices := []string{"/dev/d1", "/dev/d2"}
	mpathDevice := "/dev/" + mpathDeviceName
	fsType := "ext4"
	dummyError := errors.New("Dummy error")

	testCases := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{
			name: "fail no VolumeId",
			testFunc: func(t *testing.T) {
				node := d
				expandRequest := &csi.NodeExpandVolumeRequest{
					VolumePath:        volumePath,
					StagingTargetPath: stagingTargetPath,
				}

				_, err := node.NodeExpandVolume(context.TODO(), expandRequest)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "fail no VolumePath",
			testFunc: func(t *testing.T) {
				node := d
				expandRequest := &csi.NodeExpandVolumeRequest{
					VolumeId:          volId,
					StagingTargetPath: stagingTargetPath,
				}

				_, err := node.NodeExpandVolume(context.TODO(), expandRequest)
				assertError(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "get multipath device fail",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceConHelper := mocks.NewMockOsDeviceConnectivityHelperScsiGenericInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceExpand(mockNodeUtils, mockOsDeviceConHelper, mockMounter)

				mockOsDeviceConHelper.EXPECT().GetMpathDevice(volId).Return("", dummyError)

				_, err := node.NodeExpandVolume(context.TODO(), expandRequest)
				assertError(t, err, codes.Internal)
			},
		},
		{
			name: "get sys devices fail",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceConHelper := mocks.NewMockOsDeviceConnectivityHelperScsiGenericInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceExpand(mockNodeUtils, mockOsDeviceConHelper, mockMounter)

				mockOsDeviceConHelper.EXPECT().GetMpathDevice(volId).Return(mpathDevice, nil)
				mockNodeUtils.EXPECT().GetSysDevicesFromMpath(mpathDeviceName).Return("", dummyError)

				_, err := node.NodeExpandVolume(context.TODO(), expandRequest)
				assertError(t, err, codes.Internal)
			},
		},
		{
			name: "rescan fail",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceConHelper := mocks.NewMockOsDeviceConnectivityHelperScsiGenericInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceExpand(mockNodeUtils, mockOsDeviceConHelper, mockMounter)

				mockOsDeviceConHelper.EXPECT().GetMpathDevice(volId).Return(mpathDevice, nil)
				mockNodeUtils.EXPECT().GetSysDevicesFromMpath(mpathDeviceName).Return(rawSysDevices, nil)
				mockNodeUtils.EXPECT().RescanPhysicalDevices(sysDevices).Return(dummyError)

				_, err := node.NodeExpandVolume(context.TODO(), expandRequest)
				assertError(t, err, codes.Internal)
			},
		},
		{
			name: "expand multipath device fail",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceConHelper := mocks.NewMockOsDeviceConnectivityHelperScsiGenericInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceExpand(mockNodeUtils, mockOsDeviceConHelper, mockMounter)

				mockOsDeviceConHelper.EXPECT().GetMpathDevice(volId).Return(mpathDevice, nil)
				mockNodeUtils.EXPECT().GetSysDevicesFromMpath(mpathDeviceName).Return(rawSysDevices, nil)
				mockNodeUtils.EXPECT().RescanPhysicalDevices(sysDevices)
				mockNodeUtils.EXPECT().ExpandMpathDevice(mpathDeviceName).Return(dummyError)

				_, err := node.NodeExpandVolume(context.TODO(), expandRequest)
				assertError(t, err, codes.Internal)
			},
		},
		{
			name: "get disk format fail",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceConHelper := mocks.NewMockOsDeviceConnectivityHelperScsiGenericInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceExpand(mockNodeUtils, mockOsDeviceConHelper, mockMounter)

				mockOsDeviceConHelper.EXPECT().GetMpathDevice(volId).Return(mpathDevice, nil)
				mockNodeUtils.EXPECT().GetSysDevicesFromMpath(mpathDeviceName).Return(rawSysDevices, nil)
				mockNodeUtils.EXPECT().RescanPhysicalDevices(sysDevices)
				mockNodeUtils.EXPECT().ExpandMpathDevice(mpathDeviceName)
				mockMounter.EXPECT().GetDiskFormat(mpathDevice).Return("", dummyError)

				_, err := node.NodeExpandVolume(context.TODO(), expandRequest)
				assertError(t, err, codes.Internal)
			},
		},
		{
			name: "expand filesystem fail",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceConHelper := mocks.NewMockOsDeviceConnectivityHelperScsiGenericInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceExpand(mockNodeUtils, mockOsDeviceConHelper, mockMounter)

				mockOsDeviceConHelper.EXPECT().GetMpathDevice(volId).Return(mpathDevice, nil)
				mockNodeUtils.EXPECT().GetSysDevicesFromMpath(mpathDeviceName).Return(rawSysDevices, nil)
				mockNodeUtils.EXPECT().RescanPhysicalDevices(sysDevices)
				mockNodeUtils.EXPECT().ExpandMpathDevice(mpathDeviceName)
				mockMounter.EXPECT().GetDiskFormat(mpathDevice).Return(fsType, nil)
				mockNodeUtils.EXPECT().ExpandFilesystem(mpathDevice, stagingTargetPath, fsType).Return(dummyError)

				_, err := node.NodeExpandVolume(context.TODO(), expandRequest)
				assertError(t, err, codes.Internal)
			},
		},
		{
			name: "success expand volume",
			testFunc: func(t *testing.T) {
				mockCtl := gomock.NewController(t)
				defer mockCtl.Finish()
				mockNodeUtils := mocks.NewMockNodeUtilsInterface(mockCtl)
				mockOsDeviceConHelper := mocks.NewMockOsDeviceConnectivityHelperScsiGenericInterface(mockCtl)
				mockMounter := mocks.NewMockNodeMounter(mockCtl)
				node := newTestNodeServiceExpand(mockNodeUtils, mockOsDeviceConHelper, mockMounter)

				mockOsDeviceConHelper.EXPECT().GetMpathDevice(volId).Return(mpathDevice, nil)
				mockNodeUtils.EXPECT().GetSysDevicesFromMpath(mpathDeviceName).Return(rawSysDevices, nil)
				mockNodeUtils.EXPECT().RescanPhysicalDevices(sysDevices)
				mockNodeUtils.EXPECT().ExpandMpathDevice(mpathDeviceName)
				mockMounter.EXPECT().GetDiskFormat(mpathDevice).Return(fsType, nil)
				mockNodeUtils.EXPECT().ExpandFilesystem(mpathDevice, stagingTargetPath, fsType)

				_, err := node.NodeExpandVolume(context.TODO(), expandRequest)
				if err != nil {
					t.Fatalf("Expect no error but got: %v", err)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, tc.testFunc)
	}
}

func TestNodeGetCapabilities(t *testing.T) {
	req := &csi.NodeGetCapabilitiesRequest{}

	d := newTestNodeService(nil, nil, nil)

	caps := []*csi.NodeServiceCapability{
		{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
				},
			},
		},
		{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
				},
			},
		},
	}
	expResp := &csi.NodeGetCapabilitiesResponse{Capabilities: caps}

	resp, err := d.NodeGetCapabilities(context.TODO(), req)
	if err != nil {
		srvErr, ok := status.FromError(err)
		if !ok {
			t.Fatalf("Could not get error status code from error: %v", srvErr)
		}
		t.Fatalf("Expected nil error, got %d message %s", srvErr.Code(), srvErr.Message())
	}
	if !reflect.DeepEqual(expResp, resp) {
		t.Fatalf("Expected response {%+v}, got {%+v}", expResp, resp)
	}
}

func TestNodeGetInfo(t *testing.T) {
	topologySegments := map[string]string{"topology.kubernetes.io/zone": "testZone"}

	testCases := []struct {
		name              string
		return_iqn        string
		return_iqn_err    error
		return_fcs        []string
		return_fc_err     error
		return_nodeId_err error
		expErr            error
		expNodeId         string
		iscsiExists       bool
		fcExists          bool
	}{
		{
			name:          "good iqn, empty fc with error from node_utils",
			return_fc_err: fmt.Errorf("some error"),
			expErr:        status.Error(codes.Internal, fmt.Errorf("some error").Error()),
			iscsiExists:   true,
			fcExists:      true,
		},
		{
			name:        "empty iqn with error, one fc port",
			return_fcs:  []string{"10000000c9934d9f"},
			expNodeId:   "test-host;10000000c9934d9f",
			iscsiExists: true,
			fcExists:    true,
		},
		{
			name:        "empty iqn with error from node_utils, one more fc ports",
			return_iqn:  "",
			return_fcs:  []string{"10000000c9934d9f", "10000000c9934d9h"},
			expNodeId:   "test-host;10000000c9934d9f:10000000c9934d9h",
			iscsiExists: true,
			fcExists:    true,
		},
		{
			name:        "good iqn and good fcs",
			return_iqn:  "iqn.1994-07.com.redhat:e123456789",
			return_fcs:  []string{"10000000c9934d9f", "10000000c9934d9h"},
			expNodeId:   "test-host;10000000c9934d9f:10000000c9934d9h;iqn.1994-07.com.redhat:e123456789",
			iscsiExists: true,
			fcExists:    true,
		},
		{
			name:        "iqn and fc path are inexistent",
			iscsiExists: false,
			fcExists:    false,
			expErr:      status.Error(codes.Internal, fmt.Errorf("Cannot find valid fc wwns or iscsi iqn").Error()),
		},
		{
			name:        "iqn path is inexistsent",
			iscsiExists: false,
			fcExists:    true,
			return_fcs:  []string{"10000000c9934d9f"},
			expNodeId:   "test-host;10000000c9934d9f",
		},
		{
			name:        "fc path is inexistent",
			iscsiExists: true,
			fcExists:    false,
			return_iqn:  "iqn.1994-07.com.redhat:e123456789",
			expNodeId:   "test-host;;iqn.1994-07.com.redhat:e123456789",
		}, {
			name:              "generate NodeID returns error",
			return_iqn:        "iqn.1994-07.com.redhat:e123456789",
			return_fcs:        []string{"10000000c9934d9f", "10000000c9934d9h"},
			return_nodeId_err: fmt.Errorf("some error"),
			expErr:            status.Error(codes.Internal, fmt.Errorf("some error").Error()),
			iscsiExists:       true,
			fcExists:          true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &csi.NodeGetInfoRequest{}

			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			fake_nodeutils := mocks.NewMockNodeUtilsInterface(mockCtrl)
			d := newTestNodeService(fake_nodeutils, nil, nil)
			fake_nodeutils.EXPECT().GetTopologyLabels(context.TODO(), d.Hostname).Return(topologySegments, nil)
			fake_nodeutils.EXPECT().IsFCExists().Return(tc.fcExists)
			if tc.fcExists {
				fake_nodeutils.EXPECT().ParseFCPorts().Return(tc.return_fcs, tc.return_fc_err)
			}
			if tc.return_fc_err == nil {
				fake_nodeutils.EXPECT().IsPathExists(driver.IscsiFullPath).Return(tc.iscsiExists)
				if tc.iscsiExists {
					fake_nodeutils.EXPECT().ParseIscsiInitiators().Return(tc.return_iqn, tc.return_iqn_err)
				}
			}

			if (tc.iscsiExists || tc.fcExists) && tc.return_fc_err == nil {
				fake_nodeutils.EXPECT().GenerateNodeID("test-host", tc.return_fcs, tc.return_iqn).Return(tc.expNodeId, tc.return_nodeId_err)
			}

			expTopology := &csi.Topology{Segments: topologySegments}
			expResponse := &csi.NodeGetInfoResponse{NodeId: tc.expNodeId, AccessibleTopology: expTopology}

			res, err := d.NodeGetInfo(context.TODO(), req)
			if tc.expErr != nil {
				if err == nil {
					t.Fatalf("Expected error to be thrown : {%v}", tc.expErr)
				} else {
					if err.Error() != tc.expErr.Error() {
						t.Fatalf("Expected error : {%v} to be equal to expected error : {%v}", err, tc.expErr)
					}
				}
			} else {
				if !reflect.DeepEqual(res, expResponse) {
					t.Fatalf("Expected res : {%v}, and got {%v}", expResponse, res)
				}
			}
		})
	}
}

func assertError(t *testing.T, err error, expectedErrorCode codes.Code) {
	if err == nil {
		t.Fatalf("Expected error code %d, got success", expectedErrorCode)
	}
	grpcError, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Failed getting error code from error: %v", grpcError)
	}
	if grpcError.Code() != expectedErrorCode {
		t.Fatalf("Expected error code %d, got %d. Error: %s", expectedErrorCode, grpcError.Code(), grpcError.Message())
	}
}

// To some files/dirs pod cannot access using its real path. It has to use a different path which is <prefix>/<path>.
// E.g. in order to access /etc/test.txt pod has to use /host/etc/test.txt
func GetPodPath(filepath string) string {
	return path.Join(driver.PrefixChrootOfHostRoot, filepath)
}
