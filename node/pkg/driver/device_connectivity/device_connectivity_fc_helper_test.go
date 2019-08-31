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

package device_connectivity_test

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	gomock "github.com/golang/mock/gomock"
	mocks "github.com/ibm/ibm-block-csi-driver/node/mocks"
	"github.com/ibm/ibm-block-csi-driver/node/pkg/driver/device_connectivity"
)

func TestHelperWaitForPathToExist_Fc(t *testing.T) {
	testCases := []struct {
		name          string
		fpaths        []string
		expErr        error
		expFound      bool
		globReturnErr error
	}{
		{
			name:          "Should fail when Glob return error",
			fpaths:        nil,
			globReturnErr: fmt.Errorf("error"),
			expErr:        fmt.Errorf("error"),
			expFound:      false,
		},
		{
			name:          "Should fail when Glob succeed but with no paths",
			fpaths:        nil,
			globReturnErr: nil,
			expErr:        os.ErrNotExist,
			expFound:      false,
		},
		{
			name:          "Should fail when Glob return error",
			fpaths:        []string{"/a/a", "/a/b"},
			globReturnErr: nil,
			expErr:        nil,
			expFound:      true,
		},
	}

	for _, tc := range testCases {

		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			fake_executer := mocks.NewMockExecuterInterface(mockCtrl)
			devicePath := "/dev/disk/by-path/pci-fc-ARRAYWWN-lun-LUNID"
			fake_executer.EXPECT().FilepathGlob(devicePath).Return(tc.fpaths, tc.globReturnErr)
			helperFc := device_connectivity.NewOsDeviceConnectivityHelperFc(fake_executer)

			_, found, err := helperFc.WaitForPathToExist(devicePath, 1, 1)
			if err != nil {
				if err.Error() != tc.expErr.Error() {
					t.Fatalf("Expected error code %s, got %s", tc.expErr, err.Error())
				}
			}
			if found != tc.expFound {
				t.Fatalf("Expected found boolean code %t, got %t", tc.expFound, found)
			}

		})
	}
}

func TestHelperGetMultipathDisk_Fc(t *testing.T) {
	testCases := []struct {
		name                 string
		osReadLinkReturnPath string
		osReadLinkReturnExc  error
		globReturns          []globReturn

		expErr     error
		expPath    string
		expErrType reflect.Type
	}{
		{
			name:                 "Should fail to if OsReadlink return error",
			osReadLinkReturnPath: "",
			osReadLinkReturnExc:  fmt.Errorf("error"),
			globReturns:          nil,

			expErr:  fmt.Errorf("error"),
			expPath: "",
		},
		{
			name:                 "Succeed if OsReadlink return md-* device instead of sdX",
			osReadLinkReturnPath: "../../dm-4",
			osReadLinkReturnExc:  nil,
			globReturns:          nil,

			expErr:  nil,
			expPath: "dm-4",
		},

		{
			name:                 "Fail if OsReadlink return sdX but FilepathGlob failed",
			osReadLinkReturnPath: "../../sdb",
			osReadLinkReturnExc:  nil,
			globReturns: []globReturn{
				globReturn{
					globReturnfpaths: nil,
					globReturnErr:    fmt.Errorf("error"),
				},
			},

			expErr:  fmt.Errorf("error"),
			expPath: "",
		},
		{
			name:                 "Fail if OsReadlink return sdX but FilepathGlob not show any of the sdX",
			osReadLinkReturnPath: "../../sdb",
			osReadLinkReturnExc:  nil,

			globReturns: []globReturn{
				globReturn{
					globReturnfpaths: []string{"/sys/block/dm-4"},
					globReturnErr:    nil,
				},
				globReturn{
					globReturnfpaths: nil, // so no dm devices found at all /sys/block/dm-*/slaves
					globReturnErr:    nil,
				},
			},

			expErrType: reflect.TypeOf(&device_connectivity.MultipleDeviceNotFoundError{}),
			expPath:    "",
		},

		{
			name:                 "Should succeed to find the /dev/dm-4",
			osReadLinkReturnPath: "../../sdb",
			osReadLinkReturnExc:  nil,

			globReturns: []globReturn{
				globReturn{
					globReturnfpaths: []string{"/sys/block/dm-4"},
					globReturnErr:    nil,
				},
				globReturn{
					globReturnfpaths: []string{"/sys/block/dm-4/slaves/sdb"},
					globReturnErr:    nil,
				},
			},

			expErr:  nil,
			expPath: "/dev/dm-4",
		},
	}

	for _, tc := range testCases {

		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			path := "/dev/disk/by-path/pci-fc-wwn:5"
			fake_executer := mocks.NewMockExecuterInterface(mockCtrl)
			fake_executer.EXPECT().OsReadlink(path).Return(tc.osReadLinkReturnPath, tc.osReadLinkReturnExc)

			if len(tc.globReturns) == 1 {
				fake_executer.EXPECT().FilepathGlob("/sys/block/dm-*").Return(tc.globReturns[0].globReturnfpaths, tc.globReturns[0].globReturnErr)
			} else if len(tc.globReturns) == 2 {
				first := fake_executer.EXPECT().FilepathGlob("/sys/block/dm-*").Return(tc.globReturns[0].globReturnfpaths, tc.globReturns[0].globReturnErr)
				second := fake_executer.EXPECT().FilepathGlob("/sys/block/dm-4/slaves/*").Return(tc.globReturns[1].globReturnfpaths, tc.globReturns[1].globReturnErr)

				gomock.InOrder(first, second)
			}
			helperFc := device_connectivity.NewOsDeviceConnectivityHelperFc(fake_executer)

			returnPath, err := helperFc.GetMultipathDisk(path)
			if tc.expErr != nil || tc.expErrType != nil {
				if err == nil {
					t.Fatalf("Expected to fail with error, got success.")
				}
				if tc.expErrType != nil {
					if reflect.TypeOf(err) != tc.expErrType {
						t.Fatalf("Expected error type %v, got different error %v", tc.expErrType, reflect.TypeOf(err))
					}
				} else {
					if err.Error() != tc.expErr.Error() {
						t.Fatalf("Expected error code %s, got %s", tc.expErr, err.Error())
					}
				}
			}
			if returnPath != tc.expPath {
				t.Fatalf("Expected found multipath device %s, got %s", tc.expPath, returnPath)
			}

		})
	}
}

func TestHelperGetFcHostIDs(t *testing.T) {
	testCases := []struct {
		name                  string
		ioutilReadFileReturns []ioutilReadFileReturn
		arrayIdentifier       string

		expErrType        reflect.Type
		expErr            error
		expHostList       []int
		globReturnMatches []string
		globReturnErr     error
	}{
		{
			name:              "Should fail when FilepathGlob return error",
			arrayIdentifier:   "fakeWWN",
			globReturnMatches: nil,
			globReturnErr:     fmt.Errorf("error"),
			expErr:            fmt.Errorf("error"),
			expHostList:       nil,
		},
		{
			name:              "Should fail when FilepathGlob return without any hosts target files at all",
			arrayIdentifier:   "fakeWWN",
			globReturnMatches: nil,
			globReturnErr:     nil,

			expErrType:  reflect.TypeOf(&device_connectivity.ConnectivityFcHostTargetNotFoundError{}),
			expHostList: nil,
		},
		{
			name: "Should fail when all port state is offline",
			ioutilReadFileReturns: []ioutilReadFileReturn{
				ioutilReadFileReturn{
					ReadFileParam: "/sys/class/fc_host/host33/port_state",
					data:          []byte("offline"),
					err:           nil,
				},
				ioutilReadFileReturn{
					ReadFileParam: "/sys/class/fc_host/host34/port_state",
					data:          []byte("offline"),
					err:           nil,
				},
			},
			arrayIdentifier: "fakeWWN",
			globReturnMatches: []string{
				"/sys/class/fc_host/host33/port_state",
				"/sys/class/fc_host/host34/port_state",
			},
			globReturnErr: nil,

			expErrType:  reflect.TypeOf(&device_connectivity.ConnectivityFcHostTargetNotFoundError{}),
			expHostList: nil,
		},

		{
			name: "Should succeed to find host33 and host34(host 35 offline, hott36 return error)",
			ioutilReadFileReturns: []ioutilReadFileReturn{
				ioutilReadFileReturn{
					ReadFileParam: "/sys/class/fc_host/host33/port_state",
					data:          []byte("online"),
					err:           nil,
				},
				ioutilReadFileReturn{
					ReadFileParam: "/sys/class/fc_host/host34/port_state",
					data:          []byte("Online"),
					err:           nil,
				},
				ioutilReadFileReturn{
					ReadFileParam: "/sys/class/fc_host/host35/port_state",
					data:          []byte("Offline"),
					err:           nil,
				},
				ioutilReadFileReturn{
					ReadFileParam: "/sys/class/fc_host/host36/port_state",
					data:          nil,
					err:           fmt.Errorf("error"),
				},
			},
			arrayIdentifier: "fakeWWN",

			globReturnMatches: []string{
				"/sys/class/fc_host/host33/port_state",
				"/sys/class/fc_host/host34/port_state",
				"/sys/class/fc_host/host35/port_state",
				"/sys/class/fc_host/host36/port_state",
			},
			globReturnErr: nil,

			expErrType:  nil,
			expHostList: []int{33, 34},
		},
	}

	for _, tc := range testCases {

		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			fake_executer := mocks.NewMockExecuterInterface(mockCtrl)

			fake_executer.EXPECT().FilepathGlob(device_connectivity.FC_HOST_SYSFS_PATH+"/host*/port_state").Return(tc.globReturnMatches, tc.globReturnErr)

			var mcalls []*gomock.Call
			for _, r := range tc.ioutilReadFileReturns {
				call := fake_executer.EXPECT().IoutilReadFile(r.ReadFileParam).Return(r.data, r.err)
				mcalls = append(mcalls, call)
			}
			gomock.InOrder(mcalls...)

			helperFc := device_connectivity.NewOsDeviceConnectivityHelperFc(fake_executer)

			returnHostList, err := helperFc.GetFcHostIDs()
			if tc.expErr != nil || tc.expErrType != nil {
				if err == nil {
					t.Fatalf("Expected to fail with error, got success.")
				}
				if tc.expErrType != nil {
					if reflect.TypeOf(err) != tc.expErrType {
						t.Fatalf("Expected error type %v, got different error %v", tc.expErrType, reflect.TypeOf(err))
					}
				} else {
					if err.Error() != tc.expErr.Error() {
						t.Fatalf("Expected error code %s, got %s", tc.expErr, err.Error())
					}
				}
			}

			if len(tc.expHostList) == 0 && len(returnHostList) == 0 {
				return
			} else if !reflect.DeepEqual(returnHostList, tc.expHostList) {
				t.Fatalf("Expected found hosts dirs %v, got %v", tc.expHostList, returnHostList)
			}

		})
	}
}
