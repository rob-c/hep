// Copyright ©2017 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fastjet

import "fmt"

// JetAlgorithm defines the algorithm used for clustering jets
type JetAlgorithm int

const (
	UndefinedJetAlgorithm JetAlgorithm = iota
	KtAlgorithm
	CambridgeAlgorithm
	AntiKtAlgorithm
	GenKtAlgorithm
	CambridgeForPassiveAlgorithm
	GenKtForPassiveAlgorithm
	EeKtAlgorithm
	EeGenKtAlgorithm
	PluginAlgorithm

	AachenAlgorithm          = CambridgeAlgorithm
	CambridgeAachenAlgorithm = CambridgeAlgorithm
)

func (alg JetAlgorithm) String() string {
	switch alg {
	case UndefinedJetAlgorithm:
		return "undefined"
	case KtAlgorithm:
		return "kt"
	case CambridgeAlgorithm:
		return "cambridge"
	case AntiKtAlgorithm:
		return "antikt"
	case GenKtAlgorithm:
		return "genkt"
	case CambridgeForPassiveAlgorithm:
		return "cambridge-for-passive"
	case GenKtForPassiveAlgorithm:
		return "genkt-for-passive"
	case EeKtAlgorithm:
		return "ee-kt"
	case EeGenKtAlgorithm:
		return "ee-genkt"
	case PluginAlgorithm:
		return "plugin"
	default:
		panic(fmt.Errorf("fastjet: invalid JetAlgorithm (%d)", int(alg)))
	}
}

// usesRapPhi reports whether the algorithm measures inter-particle distances
// in the rapidity-azimuth plane, as the hadron collider algorithms do. The
// e+e- ones use the opening angle instead, and a plugin may use anything.
func (alg JetAlgorithm) usesRapPhi() bool {
	switch alg {
	case KtAlgorithm, CambridgeAlgorithm, AntiKtAlgorithm, GenKtAlgorithm,
		CambridgeForPassiveAlgorithm, GenKtForPassiveAlgorithm:
		return true
	}
	return false
}
