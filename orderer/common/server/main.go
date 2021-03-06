/*
Copyright IBM Corp. 2017 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package server

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	_ "net/http/pprof" // This is essentially the main package for the orderer
	"os"
	"time"

	"github.com/hyperledger/fabric/common/channelconfig"
	"github.com/hyperledger/fabric/common/crypto"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/common/ledger/blockledger"
	"github.com/hyperledger/fabric/common/tools/configtxgen/encoder"
	genesisconfig "github.com/hyperledger/fabric/common/tools/configtxgen/localconfig"
	"github.com/hyperledger/fabric/core/comm"
	"github.com/hyperledger/fabric/msp"
	"github.com/hyperledger/fabric/orderer/common/bootstrap/file"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.com/hyperledger/fabric/orderer/common/metadata"
	"github.com/hyperledger/fabric/orderer/common/multichannel"
	"github.com/hyperledger/fabric/orderer/consensus"
	"github.com/hyperledger/fabric/orderer/consensus/kafka"
	"github.com/hyperledger/fabric/orderer/consensus/solo"
	cb "github.com/hyperledger/fabric/protos/common"
	ab "github.com/hyperledger/fabric/protos/orderer"
	"github.com/hyperledger/fabric/protos/utils"

	"github.com/hyperledger/fabric/common/localmsp"
	"github.com/hyperledger/fabric/common/util"
	mspmgmt "github.com/hyperledger/fabric/msp/mgmt"
	"github.com/hyperledger/fabric/orderer/common/performance"
	"github.com/op/go-logging"

	//解析命令行参数的工具,代码地址:https://github.com/alecthomas/kingpin/tree/v2.2.6
	"gopkg.in/alecthomas/kingpin.v2" 
)

const pkgLogID = "orderer/common/server"

var logger *logging.Logger

func init() {
	logger = flogging.MustGetLogger(pkgLogID)
}

/*********************************************************************
 *                      定义orderer命令                                * 
 * orderer start 启动一个orderer节点                                    *
 * orderer version 查看版本信息(包括fabric版本,go版本,操作系统信息,实验特性)     *
 * orderer benchmark 以测试模式启动orderer节点                           *
**********************************************************************/
var (
	app = kingpin.New("orderer", "Hyperledger Fabric orderer node")

	start     = app.Command("start", "Start the orderer node").Default()
	version   = app.Command("version", "Show version information")
	benchmark = app.Command("benchmark", "Run orderer in benchmark mode")
)

//主函数
func Main() {
	//解析命令行参数
	fullCmd := kingpin.MustParse(app.Parse(os.Args[1:]))

	//如果执行orderer version 则返回版本信息
	if fullCmd == version.FullCommand() {
		fmt.Println(metadata.GetVersionInfo())
		return
	}

	//代码地址:common/localconfig/config.go
	//主要是加载orderer.yaml配置文件	
	conf, err := config.Load()
	if err != nil {
		logger.Error("failed to parse config: ", err)
		os.Exit(1)
	}
	//设置日志格式和级别
	initializeLoggingLevel(conf)
	//设置msp
	initializeLocalMsp(conf)
	//打印orderer配置结构体	
	prettyPrintStruct(conf)
	Start(fullCmd, conf)
}

// Start provides a layer of abstraction for benchmark test
func Start(cmd string, conf *config.TopLevel) {

	signer := localmsp.NewSigner()
	//从orderer.yaml中读取配置
	serverConfig := initializeServerConfig(conf)
	//创建一个grpc服务
	grpcServer := initializeGrpcServer(conf, serverConfig)
	
	caSupport := &comm.CASupport{
		AppRootCAsByChain:     make(map[string][][]byte),
		OrdererRootCAsByChain: make(map[string][][]byte),
		ClientRootCAs:         serverConfig.SecOpts.ClientRootCAs,
	}
	tlsCallback := func(bundle *channelconfig.Bundle) {
		// only need to do this if mutual TLS is required
		if grpcServer.MutualTLSRequired() {
			logger.Debug("Executing callback to update root CAs")
			updateTrustedRoots(grpcServer, caSupport, bundle)
		}
	}

	manager := initializeMultichannelRegistrar(conf, signer, tlsCallback)
	mutualTLS := serverConfig.SecOpts.UseTLS && serverConfig.SecOpts.RequireClientCert
	//./server.go
	server := NewServer(manager, signer, &conf.Debug, conf.General.Authentication.TimeWindow, mutualTLS)

	switch cmd {
	case start.FullCommand(): // "start" command
		logger.Infof("Starting %s", metadata.GetVersionInfo())
		//利用go和pprof来分析系统性能
		initializeProfilingService(conf)
		//注册原子广播服务
		ab.RegisterAtomicBroadcastServer(grpcServer.Server(), server)
		logger.Info("Beginning to serve requests")
		//启动grpc
		grpcServer.Start()
	case benchmark.FullCommand(): // "benchmark" command
		logger.Info("Starting orderer in benchmark mode")
		benchmarkServer := performance.GetBenchmarkServer()
		benchmarkServer.RegisterService(server)
		benchmarkServer.Start()
	}
}

// 设置日志级别
func initializeLoggingLevel(conf *config.TopLevel) {
	//初始化日志格式为defaultFormat,具体格式查看:fabric-analyse/common/flogging/logging.go 第31行
	flogging.InitBackend(flogging.SetFormat(conf.General.LogFormat), os.Stderr)
	//日志级别为logging.INFO
	flogging.InitFromSpec(conf.General.LogLevel)
}

// 启动性能分析服务(Go pprof profiling service)
func initializeProfilingService(conf *config.TopLevel) {
	if conf.General.Profile.Enabled {
		go func() {
			logger.Info("Starting Go pprof profiling service on:", conf.General.Profile.Address)
			// The ListenAndServe() call does not return unless an error occurs.
			logger.Panic("Go pprof service failed:", http.ListenAndServe(conf.General.Profile.Address, nil))
		}()
	}
}

// conf目前读区的已经是orderer.yaml配置文件中的数据了
func initializeServerConfig(conf *config.TopLevel) comm.ServerConfig {
	// secure server config
	secureOpts := &comm.SecureOptions{
		UseTLS:            conf.General.TLS.Enabled, //默认没有开启tls
		RequireClientCert: conf.General.TLS.ClientAuthRequired, //false
	}
	//如果开启了tls
	if secureOpts.UseTLS {
		msg := "TLS"
		//从tls/server.crt文件中读取证书信息
		serverCertificate, err := ioutil.ReadFile(conf.General.TLS.Certificate)
		if err != nil {
			logger.Fatalf("Failed to load server Certificate file '%s' (%s)",
				conf.General.TLS.Certificate, err)
		}
		//从tls/server.key文件中读取私钥
		serverKey, err := ioutil.ReadFile(conf.General.TLS.PrivateKey)
		if err != nil {
			logger.Fatalf("Failed to load PrivateKey file '%s' (%s)",
				conf.General.TLS.PrivateKey, err)
		}
		var serverRootCAs, clientRootCAs [][]byte
		
		//读取根证书数据
		for _, serverRoot := range conf.General.TLS.RootCAs {
			root, err := ioutil.ReadFile(serverRoot)
			if err != nil {
				logger.Fatalf("Failed to load ServerRootCAs file '%s' (%s)",
					err, serverRoot)
			}
			serverRootCAs = append(serverRootCAs, root)
		}

		//如果需要验证客户端,则引入客户端根证书,orderer.yaml默认并没有提供该证书
		if secureOpts.RequireClientCert {
			for _, clientRoot := range conf.General.TLS.ClientRootCAs {
				root, err := ioutil.ReadFile(clientRoot)
				if err != nil {
					logger.Fatalf("Failed to load ClientRootCAs file '%s' (%s)",
						err, clientRoot)
				}
				clientRootCAs = append(clientRootCAs, root)
			}
			msg = "mutual TLS"
		}

		secureOpts.Key = serverKey
		secureOpts.Certificate = serverCertificate
		secureOpts.ServerRootCAs = serverRootCAs
		secureOpts.ClientRootCAs = clientRootCAs
		logger.Infof("Starting orderer with %s enabled", msg)
	}

	//获取keepalive设置,位于:core/comm/config.go
	//ClientInterval: 1 min
	//ClientTimeout:  20 sec - gRPC default
	//ServerInterval: 7200s - gRPC default 两次ping之间间隔7200s
	//ServerTimeout:  20s - gRPC default 在断开链接之前服务端等待客户端响应的时间
	//ServerMinInterval:  60s 客户端最小允许60s ping一次服务端,如果请求频繁,服务器会断开它们
	kaOpts := comm.DefaultKeepaliveOptions()
	// keepalive settings
	// ServerMinInterval must be greater than 0
	if conf.General.Keepalive.ServerMinInterval > time.Duration(0) {
		kaOpts.ServerMinInterval = conf.General.Keepalive.ServerMinInterval
	}
	kaOpts.ServerInterval = conf.General.Keepalive.ServerInterval
	kaOpts.ServerTimeout = conf.General.Keepalive.ServerTimeout

	return comm.ServerConfig{SecOpts: secureOpts, KaOpts: kaOpts}
}

//生成创世区块
func initializeBootstrapChannel(conf *config.TopLevel, lf blockledger.Factory) {
	var genesisBlock *cb.Block

	// 选择一个引导机制,默认provisional
	switch conf.General.GenesisMethod {
	case "provisional":
		//conf.General.GenesisProfile SampleInsecureSolo
		//加载configtx.yaml
		genesisBlock = encoder.New(genesisconfig.Load(conf.General.GenesisProfile)).GenesisBlockForChannel(conf.General.SystemChannel)
	case "file":
		//直接读取genesisblock文件
		genesisBlock = file.New(conf.General.GenesisFile).GenesisBlock()
	default:
		logger.Panic("Unknown genesis method:", conf.General.GenesisMethod)
	}
	//获取chainid
	chainID, err := utils.GetChainIDFromBlock(genesisBlock)
	if err != nil {
		logger.Fatal("Failed to parse chain ID from genesis block:", err)
	}
	//如果存在则获取该账本,如果不存在则创建一个
	gl, err := lf.GetOrCreate(chainID)
	if err != nil {
		logger.Fatal("Failed to create the system chain:", err)
	}
	//将创世区块加入到账本中
	err = gl.Append(genesisBlock)
	if err != nil {
		logger.Fatal("Could not write genesis block to ledger:", err)
	}
}

func initializeGrpcServer(conf *config.TopLevel, serverConfig comm.ServerConfig) comm.GRPCServer {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", conf.General.ListenAddress, conf.General.ListenPort))
	if err != nil {
		logger.Fatal("Failed to listen:", err)
	}

	//创建一个GRPC服务
	grpcServer, err := comm.NewGRPCServerFromListener(lis, serverConfig)
	if err != nil {
		logger.Fatal("Failed to return new GRPC server:", err)
	}

	return grpcServer
}

func initializeLocalMsp(conf *config.TopLevel) {
	//conf引用的是orderer/common/localconfig/config.go 行号:189 定义了默认值
	//conf.General.LocalMSPDir = msp
	//conf.General.BCCSP 
	//使用的是SHA256算法
	//conf.General.LocalMSPID = DEFAULT
	err := mspmgmt.LoadLocalMsp(conf.General.LocalMSPDir, conf.General.BCCSP, conf.General.LocalMSPID)
	if err != nil { // Handle errors reading the config file
		logger.Fatal("Failed to initialize local MSP:", err)
	}
}


func initializeMultichannelRegistrar(conf *config.TopLevel, signer crypto.LocalSigner,
	callbacks ...func(bundle *channelconfig.Bundle)) *multichannel.Registrar {
	
	//位置:./util.go 根据账本类型,生成账本目录,以及操作账本的方法
	lf, _ := createLedgerFactory(conf)
	// 如果还没有账本
	if len(lf.ChainIDs()) == 0 {
		//生成创世区块并加入到账本中
		initializeBootstrapChannel(conf, lf)
	} else {
		logger.Info("Not bootstrapping because of existing chains")
	}

	//设置共识机制
	consenters := make(map[string]consensus.Consenter)
	consenters["solo"] = solo.New()
	consenters["kafka"] = kafka.New(conf.Kafka)

	//TODO:
	return multichannel.NewRegistrar(lf, consenters, signer, callbacks...)
}

func updateTrustedRoots(srv comm.GRPCServer, rootCASupport *comm.CASupport,
	cm channelconfig.Resources) {
	rootCASupport.Lock()
	defer rootCASupport.Unlock()

	appRootCAs := [][]byte{}
	ordererRootCAs := [][]byte{}
	appOrgMSPs := make(map[string]struct{})
	ordOrgMSPs := make(map[string]struct{})

	if ac, ok := cm.ApplicationConfig(); ok {
		//loop through app orgs and build map of MSPIDs
		for _, appOrg := range ac.Organizations() {
			appOrgMSPs[appOrg.MSPID()] = struct{}{}
		}
	}

	if ac, ok := cm.OrdererConfig(); ok {
		//loop through orderer orgs and build map of MSPIDs
		for _, ordOrg := range ac.Organizations() {
			ordOrgMSPs[ordOrg.MSPID()] = struct{}{}
		}
	}

	if cc, ok := cm.ConsortiumsConfig(); ok {
		for _, consortium := range cc.Consortiums() {
			//loop through consortium orgs and build map of MSPIDs
			for _, consortiumOrg := range consortium.Organizations() {
				appOrgMSPs[consortiumOrg.MSPID()] = struct{}{}
			}
		}
	}

	cid := cm.ConfigtxValidator().ChainID()
	logger.Debugf("updating root CAs for channel [%s]", cid)
	msps, err := cm.MSPManager().GetMSPs()
	if err != nil {
		logger.Errorf("Error getting root CAs for channel %s (%s)", cid, err)
	}
	if err == nil {
		for k, v := range msps {
			// check to see if this is a FABRIC MSP
			if v.GetType() == msp.FABRIC {
				for _, root := range v.GetTLSRootCerts() {
					// check to see of this is an app org MSP
					if _, ok := appOrgMSPs[k]; ok {
						logger.Debugf("adding app root CAs for MSP [%s]", k)
						appRootCAs = append(appRootCAs, root)
					}
					// check to see of this is an orderer org MSP
					if _, ok := ordOrgMSPs[k]; ok {
						logger.Debugf("adding orderer root CAs for MSP [%s]", k)
						ordererRootCAs = append(ordererRootCAs, root)
					}
				}
				for _, intermediate := range v.GetTLSIntermediateCerts() {
					// check to see of this is an app org MSP
					if _, ok := appOrgMSPs[k]; ok {
						logger.Debugf("adding app root CAs for MSP [%s]", k)
						appRootCAs = append(appRootCAs, intermediate)
					}
					// check to see of this is an orderer org MSP
					if _, ok := ordOrgMSPs[k]; ok {
						logger.Debugf("adding orderer root CAs for MSP [%s]", k)
						ordererRootCAs = append(ordererRootCAs, intermediate)
					}
				}
			}
		}
		rootCASupport.AppRootCAsByChain[cid] = appRootCAs
		rootCASupport.OrdererRootCAsByChain[cid] = ordererRootCAs

		// now iterate over all roots for all app and orderer chains
		trustedRoots := [][]byte{}
		for _, roots := range rootCASupport.AppRootCAsByChain {
			trustedRoots = append(trustedRoots, roots...)
		}
		for _, roots := range rootCASupport.OrdererRootCAsByChain {
			trustedRoots = append(trustedRoots, roots...)
		}
		// also need to append statically configured root certs
		if len(rootCASupport.ClientRootCAs) > 0 {
			trustedRoots = append(trustedRoots, rootCASupport.ClientRootCAs...)
		}

		// now update the client roots for the gRPC server
		err := srv.SetClientRootCAs(trustedRoots)
		if err != nil {
			msg := "Failed to update trusted roots for orderer from latest config " +
				"block.  This orderer may not be able to communicate " +
				"with members of channel %s (%s)"
			logger.Warningf(msg, cm.ConfigtxValidator().ChainID(), err)
		}
	}
}

func prettyPrintStruct(i interface{}) {
	params := util.Flatten(i)
	var buffer bytes.Buffer
	for i := range params {
		buffer.WriteString("\n\t")
		buffer.WriteString(params[i])
	}
	logger.Infof("Orderer config values:%s\n", buffer.String())
}
