package types

import (
	"math"

	yaml "gopkg.in/yaml.v2"

	"github.com/tendermint/tendermint/crypto/tmhash"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmproto "github.com/tendermint/tendermint/proto/types"
	tmtypes "github.com/tendermint/tendermint/types"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	evidenceexported "github.com/cosmos/cosmos-sdk/x/evidence/exported"
	clientexported "github.com/cosmos/cosmos-sdk/x/ibc/02-client/exported"
	clienttypes "github.com/cosmos/cosmos-sdk/x/ibc/02-client/types"
	host "github.com/cosmos/cosmos-sdk/x/ibc/24-host"
)

var (
	_ evidenceexported.Evidence   = (*Evidence)(nil)
	_ clientexported.Misbehaviour = (*Evidence)(nil)
)

// GetClientID returns the ID of the client that committed a misbehaviour.
func (ev Evidence) GetClientID() string {
	return ev.ClientID
}

// ClientType is Tendermint light client
func (ev Evidence) ClientType() clientexported.ClientType {
	return clientexported.Tendermint
}

// Route implements Evidence interface
func (ev Evidence) Route() string {
	return clienttypes.SubModuleName
}

// String implements Evidence interface
func (ev Evidence) String() string {
	// FIXME: implement custom marshaller
	bz, err := yaml.Marshal(ev)
	if err != nil {
		panic(err)
	}
	return string(bz)
}

// Type implements Evidence interface
func (ev Evidence) Type() string {
	return "client_misbehaviour"
}

// Hash implements Evidence interface
func (ev Evidence) Hash() tmbytes.HexBytes {
	bz, err := ev.Marshal()
	if err != nil {
		panic(err)
	}
	return tmhash.Sum(bz)
}

// GetHeight returns the height at which misbehaviour occurred
//
// NOTE: assumes that evidence headers have the same height
func (ev Evidence) GetHeight() int64 {
	return int64(math.Min(float64(ev.Header1.GetHeight()), float64(ev.Header2.GetHeight())))
}

// ValidateBasic implements Evidence interface
func (ev Evidence) ValidateBasic() error {
	if err := host.DefaultClientIdentifierValidator(ev.ClientID); err != nil {
		return sdkerrors.Wrap(clienttypes.ErrInvalidEvidence, err.Error())
	}

	// ValidateBasic on both validators
	if err := ev.Header1.ValidateBasic(ev.Header1.SignedHeader.GetHeader().GetChainID()); err != nil {
		return sdkerrors.Wrap(
			clienttypes.ErrInvalidEvidence,
			sdkerrors.Wrap(err, "header 1 failed validation").Error(),
		)
	}
	if err := ev.Header2.ValidateBasic(ev.Header2.SignedHeader.GetHeader().GetChainID()); err != nil {
		return sdkerrors.Wrap(
			clienttypes.ErrInvalidEvidence,
			sdkerrors.Wrap(err, "header 2 failed validation").Error(),
		)
	}

	// Ensure that Heights are the same
	if ev.Header1.GetHeight() != ev.Header2.GetHeight() {
		return sdkerrors.Wrapf(clienttypes.ErrInvalidEvidence, "headers in evidence are on different heights (%d ≠ %d)", ev.Header1.GetHeight(), ev.Header2.GetHeight())
	}
	// Ensure that Commit Hashes are different
	if ev.Header1.SignedHeader.Commit.BlockID.Equal(ev.Header2.SignedHeader.Commit.BlockID) {
		return sdkerrors.Wrap(clienttypes.ErrInvalidEvidence, "headers commit to same blockID")
	}
	if err := ValidCommit(ev.ChainID, ev.Header1.SignedHeader.Commit, ev.Header1.ValidatorSet); err != nil {
		return err
	}
	return ValidCommit(ev.ChainID, ev.Header2.SignedHeader.Commit, ev.Header2.ValidatorSet)
}

// ValidCommit checks if the given commit is a valid commit from the passed-in validatorset
//
// CommitToVoteSet will panic if the commit cannot be converted to a valid voteset given the validatorset
// This implies that someone tried to submit evidence that wasn't actually committed by the validatorset
// thus we should return an error here and reject the evidence rather than panicing.
func ValidCommit(chainID string, commit *tmproto.Commit, valSet *tmproto.ValidatorSet) (err error) {
	var (
		tmCommit tmtypes.Commit
		tmValSet tmtypes.ValidatorSet
	)

	defer func() {
		if r := recover(); r != nil {
			err = sdkerrors.Wrapf(clienttypes.ErrInvalidEvidence, "invalid commit: %v", r)
		}
	}()

	if err := tmCommit.FromProto(commit); err != nil {
		return err
	}

	if err := tmValSet.FromProto(valSet); err != nil {
		return err
	}

	// Convert commits to vote-sets given the validator set so we can check if they both have 2/3 power
	voteSet := tmtypes.CommitToVoteSet(chainID, &tmCommit, &tmValSet)

	blockID, ok := voteSet.TwoThirdsMajority()

	// Check that ValidatorSet did indeed commit to blockID in Commit
	if !ok || !blockID.Equals(tmCommit.BlockID) {
		return sdkerrors.Wrap(clienttypes.ErrInvalidEvidence, "validator set did not commit to header 1")
	}

	return nil
}
