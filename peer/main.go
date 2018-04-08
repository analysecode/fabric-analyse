/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

/**
  该文件的重要目的是定义了peer命令,以及加载msp相关配置文件
**/

package main

import (
	"fmt"
	_ "net/http/pprof"
	"os"
	"runtime"
	"strings"

	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/core/config"
	"github.com/hyperledger/fabric/msp"
	"github.com/hyperledger/fabric/peer/chaincode"
	"github.com/hyperledger/fabric/peer/channel"
	"github.com/hyperledger/fabric/peer/clilogging"
	"github.com/hyperledger/fabric/peer/common"
	"github.com/hyperledger/fabric/peer/node"
	"github.com/hyperledger/fabric/peer/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var logger = flogging.MustGetLogger("main")
var logOutput = os.Stderr

// Constants go here.
const cmdRoot = "core"

// The main command describes the service and
// defaults to printing the help message.
var mainCmd = &cobra.Command{
	Use: "peer",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// check for --logging-level pflag first, which should override all other
		// log settings. if --logging-level is not set, use CORE_LOGGING_LEVEL
		// (environment variable takes priority; otherwise, the value set in
		// core.yaml)
		var loggingSpec string
		if viper.GetString("logging_level") != "" {
			loggingSpec = viper.GetString("logging_level")
		} else {
			loggingSpec = viper.GetString("logging.level")
		}
		flogging.InitFromSpec(loggingSpec)

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		if versionFlag {
			fmt.Print(version.GetInfo())
		} else {
			cmd.HelpFunc()(cmd, args)
		}
	},
}

// Peer command version flag
var versionFlag bool

func main() {

	//命令行第三方库viper,github地址:github.com/spf13/viper 
	//设置环境变量
	viper.SetEnvPrefix(cmdRoot)
	viper.AutomaticEnv()
	replacer := strings.NewReplacer(".", "_")
	viper.SetEnvKeyReplacer(replacer)

	/**
	  定义二级命令
	  peer version 版本命令
	  peer node 节点命令
	  peer chaincode 有关合约的命令
	  peer clilogging 有关日志的命令
	  peer channel  有关通道的命令
	**/
	mainFlags := mainCmd.PersistentFlags()
	mainFlags.BoolVarP(&versionFlag, "version", "v", false, "Display current version of fabric peer server")

	mainFlags.String("logging-level", "", "Default logging level and overrides, see core.yaml for full syntax")
	viper.BindPFlag("logging_level", mainFlags.Lookup("logging-level"))

	mainCmd.AddCommand(version.Cmd())
	mainCmd.AddCommand(node.Cmd())
	mainCmd.AddCommand(chaincode.Cmd(nil))
	mainCmd.AddCommand(clilogging.Cmd(nil))
	mainCmd.AddCommand(channel.Cmd(nil))

	//初始化配置文件
	//首先会检查环境变量,如果FABRIC_CFG_PATH存在,会作为配置文件目录,如果不存在会以$GOPATH为准
	//核心代码在:../core/config/config.go 方法:InitViper
	err := common.InitConfig(cmdRoot)
	if err != nil { // Handle errors reading the config file
		logger.Errorf("Fatal error when initializing %s config : %s", cmdRoot, err)
		os.Exit(1)
	}

	//文件目录:src/github.com/hyperledger/fabric/sampleconfig/core.yaml

	//设置使用几个CPU核,如果<1,则使用默认设置
	runtime.GOMAXPROCS(viper.GetInt("peer.gomaxprocs"))

	// 配置日志格式
	flogging.InitBackend(flogging.SetFormat(viper.GetString("logging.format")), logOutput)

	// 初始化MSP,很重要
	// 主要需要:
	// mspConfigPath 默认为msp
	// localMspId    默认为DEFAULT
	// localMspType  默认为bccsp
	var mspMgrConfigDir = config.GetPath("peer.mspConfigPath")
	var mspID = viper.GetString("peer.localMspId")
	var mspType = viper.GetString("peer.localMspType")
	if mspType == "" {
		mspType = msp.ProviderTypeToString(msp.FABRIC)
	}
	//加载msp启动所需配置文件,默认是:'mspConfigPath'
	err = common.InitCrypto(mspMgrConfigDir, mspID, mspType)
	if err != nil { // Handle errors reading the config file
		logger.Errorf("Cannot run peer because %s", err.Error())
		os.Exit(1)
	}
	// On failure Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if mainCmd.Execute() != nil {
		os.Exit(1)
	}
	logger.Info("Exiting.....")
}
