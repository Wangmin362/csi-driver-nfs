/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nfs

import (
	"runtime"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/klog/v2"
	mount "k8s.io/mount-utils"
)

// DriverOptions defines driver parameters specified in driver deployment
type DriverOptions struct {
	NodeID                string
	DriverName            string
	Endpoint              string
	MountPermissions      uint64
	WorkingMountDir       string
	DefaultOnDeletePolicy string
}

type Driver struct {
	// 当前驱动的名字，默认为：nfs.csi.k8s.io
	name    string
	nodeID  string
	version string
	// TODO 这玩意应该就是当前CSI插件监听的Socket路径
	endpoint         string
	mountPermissions uint64
	workingMountDir  string
	// 删除持久卷时是否删除子目录策略
	defaultOnDeletePolicy string

	//ids *identityServer
	// NodeServer实现了CSI规范的Node服务
	ns *NodeServer
	// 当前CSI插件支持的ControllerService能力
	cscap []*csi.ControllerServiceCapability
	// 当前CSI插件支持的NodeService能力
	nscap       []*csi.NodeServiceCapability
	volumeLocks *VolumeLocks
}

const (
	DefaultDriverName = "nfs.csi.k8s.io"
	// Address of the NFS server
	paramServer = "server"
	// Base directory of the NFS server to create volumes under.
	// The base directory must be a direct child of the root directory.
	// The root directory is omitted from the string, for example:
	//     "base" instead of "/base"
	paramShare = "share"
	// TODO 这个参数是拿来干嘛的？
	paramSubDir           = "subdir"
	paramOnDelete         = "ondelete"
	mountOptionsField     = "mountoptions"
	mountPermissionsField = "mountpermissions"
	pvcNameKey            = "csi.storage.k8s.io/pvc/name"
	pvcNamespaceKey       = "csi.storage.k8s.io/pvc/namespace"
	pvNameKey             = "csi.storage.k8s.io/pv/name"
	pvcNameMetadata       = "${pvc.metadata.name}"
	pvcNamespaceMetadata  = "${pvc.metadata.namespace}"
	pvNameMetadata        = "${pv.metadata.name}"
)

func NewDriver(options *DriverOptions) *Driver {
	klog.V(2).Infof("Driver: %v version: %v", options.DriverName, driverVersion)

	n := &Driver{
		name:             options.DriverName,
		version:          driverVersion,
		nodeID:           options.NodeID,
		endpoint:         options.Endpoint,
		mountPermissions: options.MountPermissions,
		workingMountDir:  options.WorkingMountDir,
	}

	n.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
		// 支持删除、创建持久卷
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		// 支持把持久卷挂载到一个节点上，允许多个Pod同事进行读写
		csi.ControllerServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
		// TODO 支持卷的克隆, NFS是如何支持卷的克隆的？
		csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
		// TODO 支持创建和删除持久卷快照,NFS是如何支持卷的快照的？
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
	})

	n.AddNodeServiceCapabilities([]csi.NodeServiceCapability_RPC_Type{
		// 支持获取持久卷的事情情况
		csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
		// 支持把持久卷挂载到耽搁节点上，允许多个Pod同时进行读写
		csi.NodeServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
		csi.NodeServiceCapability_RPC_UNKNOWN,
	})
	n.volumeLocks = NewVolumeLocks()
	return n
}

func NewNodeServer(n *Driver, mounter mount.Interface) *NodeServer {
	return &NodeServer{
		Driver:  n,
		mounter: mounter,
	}
}

func (n *Driver) Run(testMode bool) {
	// 打印当前CSI存储插件的版本信息，这玩意对于排查问题来说缺失很有必要。很多问题通常是版本不对导致的，而在排查问题的过程当中，我们通常会
	// 忽略这个因素
	versionMeta, err := GetVersionYAML(n.name)
	if err != nil {
		klog.Fatalf("%v", err)
	}
	klog.V(2).Infof("\nDRIVER INFORMATION:\n-------------------\n%s\n\nStreaming logs below:", versionMeta)

	// 用于挂在NFS
	mounter := mount.New("")
	if runtime.GOOS == "linux" {
		// MounterForceUnmounter is only implemented on Linux now
		mounter = mounter.(mount.MounterForceUnmounter)
	}
	n.ns = NewNodeServer(n, mounter)
	s := NewNonBlockingGRPCServer()
	// 启动GRPC服务，并且监听endpoint参数所指向的socket文件
	s.Start(n.endpoint,
		NewDefaultIdentityServer(n),
		// NFS plugin has not implemented ControllerServer
		// using default controllerserver.
		NewControllerServer(n),
		n.ns,
		testMode)
	s.Wait()
}

func (n *Driver) AddControllerServiceCapabilities(cl []csi.ControllerServiceCapability_RPC_Type) {
	var csc []*csi.ControllerServiceCapability
	for _, c := range cl {
		csc = append(csc, NewControllerServiceCapability(c))
	}
	n.cscap = csc
}

func (n *Driver) AddNodeServiceCapabilities(nl []csi.NodeServiceCapability_RPC_Type) {
	var nsc []*csi.NodeServiceCapability
	for _, n := range nl {
		nsc = append(nsc, NewNodeServiceCapability(n))
	}
	n.nscap = nsc
}

func IsCorruptedDir(dir string) bool {
	_, pathErr := mount.PathExists(dir)
	return pathErr != nil && mount.IsCorruptedMnt(pathErr)
}

// replaceWithMap replace key with value for str
func replaceWithMap(str string, m map[string]string) string {
	for k, v := range m {
		if k != "" {
			str = strings.ReplaceAll(str, k, v)
		}
	}
	return str
}
