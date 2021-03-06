// Copyright (c) 2018 IoTeX
// This is an alpha (internal) release and is not suitable for production. This source code is provided 'as is' and no
// warranties are given as to title or non-infringement, merchantability or fitness for purpose and, to the extent
// permitted by law, all liability for your use of the code is disclaimed. This source code is governed by Apache
// License 2.0 that can be found in the LICENSE file.

package blockchain

import (
	"io/ioutil"
	"math/big"

	"gopkg.in/yaml.v2"

	"github.com/iotexproject/iotex-core/action"
	"github.com/iotexproject/iotex-core/address"
	"github.com/iotexproject/iotex-core/config"
	"github.com/iotexproject/iotex-core/logger"
	"github.com/iotexproject/iotex-core/pkg/hash"
	"github.com/iotexproject/iotex-core/pkg/keypair"
	"github.com/iotexproject/iotex-core/pkg/util/fileutil"
	"github.com/iotexproject/iotex-core/pkg/version"
)

const testnetActionPath = "testnet_actions.yaml"

// Genesis defines the Genesis default settings
type Genesis struct {
	TotalSupply         *big.Int
	BlockReward         *big.Int
	Timestamp           uint64
	ParentHash          hash.Hash32B
	GenesisCoinbaseData string
	CreatorPubKey       string
	CreatorPrivKey      string
}

// GenesisAction is the root action struct, each package's action should be put as its sub struct
type GenesisAction struct {
	Creation       Creator     `yaml:"creator"`
	SelfNominators []Nominator `yaml:"selfNominators"`
	Transfers      []Transfer  `yaml:"transfers"`
	SubChains      []SubChain  `yaml:"subChains"`
}

// Creator is the Creator of the genesis block
type Creator struct {
	PubKey string `yaml:"pubKey"`
	PriKey string `yaml:"priKey"`
}

// Nominator is the Nominator struct for vote struct
type Nominator struct {
	PubKey string `yaml:"pubKey"`
	PriKey string `yaml:"priKey"`
}

// Transfer is the Transfer struct
type Transfer struct {
	Amount      int64  `yaml:"amount"`
	RecipientPK string `yaml:"recipientPK"`
}

// SubChain is the SubChain struct
type SubChain struct {
	ChainID            uint32 `yaml:"chainID"`
	SecurityDeposit    int64  `yaml:"securityDeposit"`
	OperationDeposit   int64  `yaml:"operationDeposit"`
	StartHeight        uint64 `yaml:"startHeight"`
	ParentHeightOffset uint64 `yaml:"parentHeightOffset"`
}

// Gen hardcodes genesis default settings
var Gen = &Genesis{
	TotalSupply:         ConvertIotxToRau(10000000000),
	BlockReward:         ConvertIotxToRau(5),
	Timestamp:           uint64(1524676419),
	ParentHash:          hash.Hash32B{},
	GenesisCoinbaseData: "Connecting the physical world, block by block",
}

// CreatorAddr returns the creator address on a particular chain
func (g *Genesis) CreatorAddr(chainID uint32) string {
	pk, _ := decodeKey(g.CreatorPubKey, "")
	return generateAddr(chainID, pk)
}

// NewGenesisBlock creates a new genesis block
func NewGenesisBlock(cfg *config.Config) *Block {
	var filePath string
	if cfg != nil && cfg.Chain.GenesisActionsPath != "" {
		filePath = cfg.Chain.GenesisActionsPath
	} else {
		filePath = fileutil.GetFileAbsPath(testnetActionPath)
	}

	actionsBytes, err := ioutil.ReadFile(filePath)
	if err != nil {
		logger.Panic().Err(err).Msg("Fail to create genesis block")
	}
	actions := GenesisAction{}
	if err := yaml.Unmarshal(actionsBytes, &actions); err != nil {
		logger.Panic().Err(err).Msg("Fail to create genesis block")
	}

	Gen.CreatorPubKey = actions.Creation.PubKey
	Gen.CreatorPrivKey = actions.Creation.PriKey
	_, creatorPrik := decodeKey(Gen.CreatorPubKey, Gen.CreatorPrivKey)
	creatorAddr := Gen.CreatorAddr(cfg.Chain.ID)

	votes := []*action.Vote{}
	for _, nominator := range actions.SelfNominators {
		pk, sk := decodeKey(nominator.PubKey, nominator.PriKey)
		address := generateAddr(cfg.Chain.ID, pk)
		vote, err := action.NewVote(
			0,
			address,
			address,
			0,
			big.NewInt(0),
		)
		if err != nil {
			logger.Panic().Err(err).Msg("Fail to create the new vote action")
		}
		if err := action.Sign(vote, sk); err != nil {
			logger.Panic().Err(err).Msg("Fail to sign the new vote action")
		}
		votes = append(votes, vote)
	}

	transfers := []*action.Transfer{}
	for _, transfer := range actions.Transfers {
		rpk, _ := decodeKey(transfer.RecipientPK, "")
		recipientAddr := generateAddr(cfg.Chain.ID, rpk)
		tsf, err := action.NewTransfer(
			0,
			ConvertIotxToRau(transfer.Amount),
			creatorAddr,
			recipientAddr,
			[]byte{},
			0,
			big.NewInt(0),
		)
		if err != nil {
			logger.Panic().Err(err).Msg("Fail to create the new transfer action")
		}
		if err := action.Sign(tsf, creatorPrik); err != nil {
			logger.Panic().Err(err).Msg("Fail to sign the new transfer action")
		}
		transfers = append(transfers, tsf)
	}

	acts := make([]action.Action, 0)
	if cfg.Chain.EnableSubChainStartInGenesis {
		for _, sc := range actions.SubChains {
			start := action.NewStartSubChain(
				0,
				sc.ChainID,
				creatorAddr,
				ConvertIotxToRau(sc.SecurityDeposit),
				ConvertIotxToRau(sc.OperationDeposit),
				sc.StartHeight,
				sc.ParentHeightOffset,
				0,
				big.NewInt(0),
			)
			if err := action.Sign(start, creatorPrik); err != nil {
				logger.Panic().Err(err).Msg("Fail to sign the new start sub-chain action")
			}
			acts = append(acts, start)
		}
	}

	block := &Block{
		Header: &BlockHeader{
			version:       version.ProtocolVersion,
			chainID:       cfg.Chain.ID,
			height:        uint64(0),
			timestamp:     Gen.Timestamp,
			prevBlockHash: Gen.ParentHash,
			txRoot:        hash.ZeroHash32B,
			stateRoot:     hash.ZeroHash32B,
			blockSig:      []byte{},
		},
		Transfers: transfers,
		Votes:     votes,
		Actions:   acts,
	}

	block.Header.txRoot = block.TxRoot()
	return block
}

// decodeKey decodes the string keypair
func decodeKey(pubK string, priK string) (pk keypair.PublicKey, sk keypair.PrivateKey) {
	if len(pubK) > 0 {
		publicKey, err := keypair.DecodePublicKey(pubK)
		if err != nil {
			logger.Panic().Err(err).Msg("Fail to decode public key")
		}
		pk = publicKey
	}
	if len(priK) > 0 {
		privateKey, err := keypair.DecodePrivateKey(priK)
		if err != nil {
			logger.Panic().Err(err).Msg("Fail to decode private key")
		}
		sk = privateKey
	}
	return
}

// generateAddr returns the string address according to public key
func generateAddr(chainID uint32, pk keypair.PublicKey) string {
	pkHash := keypair.HashPubKey(pk)
	return address.New(chainID, pkHash[:]).IotxAddress()
}
