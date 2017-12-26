package clusterserver

import (
	"fmt"
	"github.com/viphxin/xingo/cluster"
	"github.com/viphxin/xingo/fserver"
	"github.com/viphxin/xingo/iface"
	"github.com/viphxin/xingo/logger"
	"github.com/viphxin/xingo/utils"
	"sync"
)

const (
	KEEP_ALIVED_DURATION = 15  //s
	KEEP_ALIVED_CHECKTIMES =2  //重连检查次数
)

type Master struct {
	OnlineNodes map[string]bool
	Cconf       *cluster.ClusterConf
	Childs      *cluster.ChildMgr
	TelnetServer   iface.Iserver
	sync.RWMutex
}

func NewMaster(path string) *Master {
	logger.SetPrefix(fmt.Sprintf("[%s]", "MASTER"))
	cconf, err := cluster.NewClusterConf(path)
	if err != nil {
		panic("cluster conf error!!!")
	}
	GlobalMaster = &Master{
		OnlineNodes: make(map[string]bool),
		Cconf:       cconf,
		Childs:      cluster.NewChildMgr(),
	}
	//regest callback
	utils.GlobalObject.TcpPort = GlobalMaster.Cconf.Master.RootPort
	utils.GlobalObject.Protoc = cluster.NewRpcServerProtocol()
	utils.GlobalObject.RpcCProtoc = cluster.NewRpcClientProtocol()
	utils.GlobalObject.OnClusterConnectioned = DoConnectionMade
	utils.GlobalObject.OnClusterClosed = DoConnectionLost
	utils.GlobalObject.Name = "master"
	if cconf.Master.Log != "" {
		utils.GlobalObject.LogName = cconf.Master.Log
		utils.ReSettingLog()
	}

	//telnet debug tool
	if GlobalMaster.Cconf.Master.DebugPort > 0{
		if GlobalMaster.Cconf.Master.Host != ""{
			GlobalMaster.TelnetServer = fserver.NewTcpServer("telnet_server", "tcp4", GlobalMaster.Cconf.Master.Host,
				GlobalMaster.Cconf.Master.DebugPort, 100, cluster.NewTelnetProtocol())
		}else{
			GlobalMaster.TelnetServer = fserver.NewTcpServer("telnet_server", "tcp4", "127.0.0.1", GlobalMaster.Cconf.Master.DebugPort, 100, cluster.NewTelnetProtocol())
		}
		logger.Info(fmt.Sprintf("telnet tool start: %s:%d.", GlobalMaster.Cconf.Master.Host, GlobalMaster.Cconf.Master.DebugPort))
	}
	return GlobalMaster
}

func DoConnectionMade(fconn iface.Iconnection) {
	//logger.Info("node connected to master!!!")
}

func DoConnectionLost(fconn iface.Iconnection) {
	nodename, err := fconn.GetProperty("child")
	if err == nil {
		logger.Info("node :"+nodename.(string)+" disconnected from master!!!")
		GlobalMaster.RemoveNode(nodename.(string))
	}
}

func (this *Master) StartMaster() {
	s := fserver.NewServer()
	if GlobalMaster.TelnetServer != nil{
		this.TelnetServer.Start()
	}
	//check node alive tick
	//s.CallLoop(KEEP_ALIVED_DURATION*time.Second, this.CheckChildsAlive, true)
	s.Serve()
}

func (this *Master) AddRpcRouter(router interface{}) {
	//add rpc ---------------start
	utils.GlobalObject.Protoc.AddRpcRouter(router)
	//add rpc ---------------end
}

func (this *Master) AddNode(name string, writer iface.IWriter) {
	this.Lock()
	defer this.Unlock()

	this.Childs.AddChild(name, writer)
	writer.SetProperty("child", name)
	this.OnlineNodes[name] = true
}

func (this *Master) RemoveNode(name string) {
	this.Lock()
	defer this.Unlock()

	this.Childs.RemoveChild(name)
	delete(this.OnlineNodes, name)
}

func (this *Master)ChildDisconnected(name string)  {
	//节点掉线通知child节点的父节点
	remotes, err := GlobalMaster.Cconf.GetRemotesByName(name)
	if err == nil && len(remotes) > 0 {
		for _, remote := range remotes {
			remoteProxy, err := GlobalMaster.Childs.GetChild(remote)
			if err == nil {
				//child是子节点 true
				remoteProxy.CallChildNotForResult("NodeDownNtf", true, name)
			}
		}
	}
	//节点掉线通知child节点的子节点
	curChilds := GlobalMaster.Cconf.GetChildsByName(name)
	if len(curChilds) > 0 {
		for _, curChild := range curChilds {
			curChildProxy, err := GlobalMaster.Childs.GetChild(curChild)
			if err == nil {
				//child是父节点 false
				curChildProxy.CallChildNotForResult("NodeDownNtf", false, name)
			}
		}
	}
	this.Childs.RemoveChild(name)
}

func (this *Master)CheckChildsAlive(params ...interface{}) {
	childs := this.Childs.GetChilds()
	for _, child := range childs {
		go this.CheckChildAlive(child)
	}
}
func (this *Master)CheckChildAlive(child *cluster.Child){
	ch:=make(chan bool)
	check:= func() {
		for i:=0;i< KEEP_ALIVED_CHECKTIMES;i++{
			_, err := child.CallChildForResult("CheckAlive")
			if err == nil {
				ch<-true
				break
			}else{
				continue
			}
		}
		ch<-false
	}
	go check()
	if <-ch == false{
		this.ChildDisconnected(child.GetName())
	}
}