/*
Copyright IBM Corp. 2017 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package consensus

import (
	"github.com/hyperledger/fabric/common/channelconfig"
	"github.com/hyperledger/fabric/common/crypto"
	"github.com/hyperledger/fabric/orderer/common/blockcutter"
	"github.com/hyperledger/fabric/orderer/common/msgprocessor"
	cb "github.com/hyperledger/fabric/protos/common"
)

// 共识接口
type Consenter interface {
	// HandleChain should create and return a reference to a Chain for the given set of resources.
	// It will only be invoked for a given chain once per process.  In general, errors will be treated
	// as irrecoverable and cause system shutdown.  See the description of Chain for more details
	// The second argument to HandleChain is a pointer to the metadata stored on the `ORDERER` slot of
	// the last block committed to the ledger of this Chain.  For a new chain, this metadata will be
	// nil, as this field is not set on the genesis block
	HandleChain(support ConsenterSupport, metadata *cb.Metadata) (Chain, error)
}

// Chain 定义如何为ordering添加消息
// 标注, 为了更加灵活, 制定者有责任去提取排好序的消息,通过blockcutter来发送它.接收器通过HandleChain来切割区块,
// 并最终写入到账本中.
// 这种设计允许两种主流程
// 1. 消息被排序后添加到数据流, 数据流被切分到区块中, 然后区块最终被提交 (solo, kafka)
// 2. 消息被切分到区块中, 区块被排序, 接着区块被提交 (sbft)
type Chain interface {
	// 备注: kafka 共识机制已经不再更新去执行验证检查条件.  现在, Order或者配置在之前入队.
	// 这不会导致数据不一致, 但浪费了周期，并且一旦引入就需要正确支持配置更新
	//, MSP处理器中的MsgClassification逻辑应该为非配置更新或者正常的msp消息类型

	// Order 接受一个已经在给定的configSeq处理过的消息.
	// 如果 configSeq 推进, 它负责重新验证和丢失消息会返回错误,标记这个消息不被接受
	Order(env *cb.Envelope, configSeq uint64) error

	// Configure 接收一个重新配置渠道的消息如果承诺和触发的configseq更新.  这个配置必须是已经被配置更新的消息出发. 
	// 如果配置序列前进,共识器会负责重新计算得到的配置,
	// 如果重新配置不再有效，则丢弃该消息.共识器可能会返回一个错误来标记这个消息不被接受
	Configure(config *cb.Envelope, configSeq uint64) error

	// WaitReady 区块将会等待共识器做好接收消息的准备.
	WaitReady() error

	// Errored returns a channel which will close when an error has occurred.
	// This is especially useful for the Deliver client, who must terminate waiting
	// clients when the consenter is not up to date.
	Errored() <-chan struct{}

	// Start should allocate whatever resources are needed for staying up to date with the chain.
	// Typically, this involves creating a thread which reads from the ordering source, passes those
	// messages to a block cutter, and writes the resulting blocks to the ledger.
	Start()

	// Halt frees the resources which were allocated for this Chain.
	Halt()
}

// ConsenterSupport provides the resources available to a Consenter implementation.
type ConsenterSupport interface {
	crypto.LocalSigner
	msgprocessor.Processor

	// BlockCutter returns the block cutting helper for this channel.
	BlockCutter() blockcutter.Receiver

	// SharedConfig provides the shared config from the channel's current config block.
	SharedConfig() channelconfig.Orderer

	// CreateNextBlock takes a list of messages and creates the next block based on the block with highest block number committed to the ledger
	// Note that either WriteBlock or WriteConfigBlock must be called before invoking this method a second time.
	CreateNextBlock(messages []*cb.Envelope) *cb.Block

	// WriteBlock 提交区块到账本.
	WriteBlock(block *cb.Block, encodedMetadataValue []byte)

	// WriteConfigBlock 提交区块到账本, 并且把配置更新到里面.
	WriteConfigBlock(block *cb.Block, encodedMetadataValue []byte)

	// Sequence 返回当前的配置序列.
	Sequence() uint64

	// ChainID 返回通道id.
	ChainID() string

	// Height 返回区块高度.
	Height() uint64
}
