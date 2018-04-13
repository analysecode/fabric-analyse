## ORDERER SERVICE 研究记录
### orderer服务中涉及到的目录
>common/
>orderer/ 
>>>>common/
>>>>>>>>blockcutter/ 主要提供了切割账本的方法
>>>>>>>>bootstrap/ 生成创世区块的一些引导性工作,比如区块的结构
>>>>>>>>broadcast/ 有关广播消息的一些方法
>>>>>>>>localconfig/ 定义启动orderer所需要的配置项,以及加载方法
>>>>>>>>metadata/ 主要提供orderer version的信息
>>>>>>>>msgprocessor/
>>>>>>>>multichannel/ 
>>>>>>>>performance/
>>>>>>>>server/
>>>>>>>>>>>server.go - orderer grpc服务端代码。
>>>>>>>>>>>main.go - orderer初始化启动代码。

>>>>consensus/ 共识机制相关,主要提供了solo和kafaka
>>>>>>>>kafaka/
>>>>>>>>solo/
>>>>mocks/ mock测试
>>>>sample_clients/
>>>>main.go 入口文件

protos/orderer
sampleconfig/ 配置文件存放目录



###分析orderer节点的目的
一. 一个orderer节点是如何启动起来的? 都包括哪些步骤? 有哪些关键的地方需要注意?
1. 加载启动节点所需要的配置文件,默认会使用fabric/sampleconfig/orderer.yaml

关键: fabric使用了viper来进行配置文件的管理,加载配置最核心的代码core/config/config.go 中的InitViper方法

2. 启动一个grpc服务

二. 共识服务是如何进行共识的?
三. orderer节点与peer节点的关系? 以及在什么情况下会进行通信?以及如何进行通信的?



