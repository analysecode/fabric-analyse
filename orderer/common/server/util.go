/*
Copyright IBM Corp. 2017 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package server

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/hyperledger/fabric/common/ledger/blkstorage/fsblkstorage"
	"github.com/hyperledger/fabric/common/ledger/blockledger"
	fileledger "github.com/hyperledger/fabric/common/ledger/blockledger/file"
	jsonledger "github.com/hyperledger/fabric/common/ledger/blockledger/json"
	ramledger "github.com/hyperledger/fabric/common/ledger/blockledger/ram"
	config "github.com/hyperledger/fabric/orderer/common/localconfig"
)

func createLedgerFactory(conf *config.TopLevel) (blockledger.Factory, string) {
	var lf blockledger.Factory
	var ld string
	//判断账本类型
	switch conf.General.LedgerType {
	case "file":
		ld = conf.FileLedger.Location //账本所在目录:/var/hyperledger/production/orderer
		if ld == "" {
			ld = createTempDir(conf.FileLedger.Prefix)
		}

		logger.Debug("Ledger dir:", ld)

		//common/ledger/blockledger/file/factory.go
		lf = fileledger.New(ld)

		//为每一个通道都会创建一个chains目录,用来存储账本文件
		createSubDir(ld, fsblkstorage.ChainsDir)
	case "json"://以json的格式存储到账本中
		ld = conf.FileLedger.Location
		if ld == "" {
			ld = createTempDir(conf.FileLedger.Prefix)
		}
		logger.Debug("Ledger dir:", ld)
		lf = jsonledger.New(ld)
	case "ram"://直接存储在内容中
		fallthrough
	default:
		lf = ramledger.New(int(conf.RAMLedger.HistorySize))
	}
	return lf, ld
}

func createTempDir(dirPrefix string) string {
	dirPath, err := ioutil.TempDir("", dirPrefix)
	if err != nil {
		logger.Panic("Error creating temp dir:", err)
	}
	return dirPath
}

func createSubDir(parentDirPath string, subDir string) (string, bool) {
	var created bool
	subDirPath := filepath.Join(parentDirPath, subDir)
	if _, err := os.Stat(subDirPath); err != nil {
		if os.IsNotExist(err) {
			if err = os.Mkdir(subDirPath, 0755); err != nil {
				logger.Panic("Error creating sub dir:", err)
			}
			created = true
		}
	} else {
		logger.Debugf("Found %s sub-dir and using it", fsblkstorage.ChainsDir)
	}
	return subDirPath, created
}
