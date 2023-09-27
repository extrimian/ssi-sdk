package bbs

import (
	gocrypto "crypto"
	"encoding/base64"
	"strings"

	"github.com/extrimian/ssi-sdk/crypto"
	"github.com/extrimian/ssi-sdk/cryptosuite"
	. "github.com/extrimian/ssi-sdk/util"
	"github.com/goccy/go-json"
	"github.com/pkg/errors"
)

const (
	BBSPlusSignatureProof2020 cryptosuite.SignatureType = "BbsBlsSignatureProof2020" //  #nosec
)

type BBSPlusSignatureProofSuite struct{}

func GetBBSPlusSignatureProofSuite() *BBSPlusSignatureProofSuite {
	return new(BBSPlusSignatureProofSuite)
}

// CryptoSuiteInfo interface

var _ cryptosuite.CryptoSuiteInfo = (*BBSPlusSignatureProofSuite)(nil)

func (BBSPlusSignatureProofSuite) ID() string {
	return BBSPlusSignatureSuiteID
}

func (BBSPlusSignatureProofSuite) Type() cryptosuite.LDKeyType {
	return BBSPlusSignatureSuiteType
}

func (BBSPlusSignatureProofSuite) CanonicalizationAlgorithm() string {
	return BBSPlusSignatureSuiteCanonicalizationAlgorithm
}

func (BBSPlusSignatureProofSuite) MessageDigestAlgorithm() gocrypto.Hash {
	return BBSPlusSignatureSuiteDigestAlgorithm
}

func (BBSPlusSignatureProofSuite) SignatureAlgorithm() cryptosuite.SignatureType {
	return BBSPlusSignatureProof2020
}

func (BBSPlusSignatureProofSuite) RequiredContexts() []string {
	return []string{BBSSecurityContext}
}

// SelectivelyDisclose takes in a credential (parameter `p` that's WithEmbeddedProof) and a map of fields to disclose as an LD frame, and produces a map of the JSON representation of the derived credential. The derived credential only contains the information that was specified in the LD frame, and a proof that's derived from the original credential. Note that a requirement for `p` is that the property `"proof"` must be present when it's marshaled to JSON, and it's value MUST be an object that conforms to a `BBSPlusProof`.
func (b BBSPlusSignatureProofSuite) SelectivelyDisclose(v BBSPlusVerifier, p cryptosuite.WithEmbeddedProof, toDiscloseFrame map[string]any, nonce []byte) (map[string]any, error) {
	// first compact the document with the security context
	compactProvable, compactProof, err := b.compactProvable(p)
	if err != nil {
		return nil, err
	}

	deriveProofResult, err := b.CreateDeriveProof(compactProvable, toDiscloseFrame)
	if err != nil {
		return nil, err
	}

	bbsPlusProof, err := BBSPlusProofFromGenericProof(compactProof)
	if err != nil {
		return nil, err
	}

	// prepare the statements and indicies to be revealed
	statements, revealIndicies, err := b.prepareRevealData(*deriveProofResult, *bbsPlusProof)
	if err != nil {
		return nil, err
	}

	// pull of signature from original provable
	signatureBytes, err := decodeProofValue(bbsPlusProof.ProofValue)
	if err != nil {
		return nil, err
	}

	// derive the proof
	derivedProofValue, err := v.DeriveProof(statements, signatureBytes, nonce, revealIndicies)
	if err != nil {
		return nil, err
	}

	// attach the proof to the derived credential
	derivedProof := &BBSPlusSignature2020Proof{
		Type:               BBSPlusSignatureProof2020,
		Created:            bbsPlusProof.Created,
		VerificationMethod: bbsPlusProof.VerificationMethod,
		ProofPurpose:       bbsPlusProof.ProofPurpose,
		ProofValue:         base64.StdEncoding.EncodeToString(derivedProofValue),
		Nonce:              base64.StdEncoding.EncodeToString(nonce),
	}
	derivedCred := deriveProofResult.RevealedDocument
	derivedCred["proof"] = derivedProof
	return derivedCred, nil
}

func (BBSPlusSignatureProofSuite) compactProvable(p cryptosuite.WithEmbeddedProof) (cryptosuite.WithEmbeddedProof, *crypto.Proof, error) {
	var genericProvable map[string]any
	provableBytes, err := json.Marshal(p)
	if err != nil {
		return nil, nil, err
	}
	if err = json.Unmarshal(provableBytes, &genericProvable); err != nil {
		return nil, nil, errors.Wrap(err, "unmarshalling provable to generic map")
	}
	compactProvable, err := LDCompact(genericProvable, cryptosuite.W3CSecurityContext)
	if err != nil {
		return nil, nil, errors.Wrap(err, "compacting provable")
	}

	// create a copy of the proof and remove it from the provable
	compactProof := crypto.Proof(compactProvable["proof"])
	delete(compactProvable, "proof")

	// turn the compact provable back to a generic credential
	compactedProvableBytes, err := json.Marshal(compactProvable)
	if err != nil {
		return nil, nil, err
	}
	var genericCred cryptosuite.GenericProvable
	if err = json.Unmarshal(compactedProvableBytes, &genericCred); err != nil {
		return nil, nil, errors.Wrap(err, "unmarshalling compacted provable to generic credential")
	}
	return &genericCred, &compactProof, nil
}

func (b BBSPlusSignatureProofSuite) prepareRevealData(deriveProofResult DeriveProofResult, bbsPlusProof BBSPlusSignature2020Proof) (statementBytesArrays [][]byte, revealIndices []int, err error) {
	// prepare proof by removing the proof value and canonicalizing
	canonicalProofStatements, err := b.prepareBLSProof(bbsPlusProof)
	if err != nil {
		return nil, nil, err
	}

	// total # indicies to be revealed = total statements in the proof - original proof result + revealed indicies
	numProofStatements := len(canonicalProofStatements)
	revealIndices = make([]int, numProofStatements+len(deriveProofResult.RevealedIndicies))

	// add the original proof result to the beginning of the reveal indicies
	for i := range canonicalProofStatements {
		revealIndices[i] = i
	}

	// add the other statements to the indicies
	for i := range deriveProofResult.RevealedIndicies {
		revealIndices[i+numProofStatements] = numProofStatements + deriveProofResult.RevealedIndicies[i]
	}

	// turn all statements into bytes before signing
	statements := append(canonicalProofStatements, deriveProofResult.InputProofDocumentStatements...)
	statementBytesArrays = make([][]byte, len(statements))
	for i := range statements {
		statementBytesArrays[i] = []byte(statements[i])
	}
	return statementBytesArrays, revealIndices, nil
}

func (b BBSPlusSignatureProofSuite) prepareBLSProof(bbsPlusProof BBSPlusSignature2020Proof) ([]string, error) {
	// canonicalize proof after removing the proof value
	bbsPlusProof.SetProofValue("")

	marshaledProof, err := b.Marshal(bbsPlusProof)
	if err != nil {
		return nil, err
	}

	// add the security context before canonicalization
	var genericProof map[string]any
	if err = json.Unmarshal(marshaledProof, &genericProof); err != nil {
		return nil, err
	}
	genericProof["@context"] = cryptosuite.W3CSecurityContext

	proofBytes, err := json.Marshal(genericProof)
	if err != nil {
		return nil, err
	}

	canonicalProof, err := b.Canonicalize(proofBytes)
	if err != nil {
		return nil, err
	}
	return canonicalizedLDToStatements(*canonicalProof), nil
}

type DeriveProofResult struct {
	RevealedIndicies             []int
	InputProofDocumentStatements []string
	RevealedDocument             map[string]any
}

// CreateDeriveProof https://w3c-ccg.github.io/vc-di-bbs/#create-derive-proof-data-algorithm
func (b BBSPlusSignatureProofSuite) CreateDeriveProof(inputProofDocument any, revealDocument map[string]any) (*DeriveProofResult, error) {
	// 1. Apply the canonicalization algorithm to the input proof document to obtain a set of statements represented
	// as n-quads. Let this set be known as the input proof document statements.
	marshaledInputProofDoc, err := b.Marshal(inputProofDocument)
	if err != nil {
		return nil, err
	}
	inputProofDocumentStatements, err := b.Canonicalize(marshaledInputProofDoc)
	if err != nil {
		return nil, err
	}

	// 2. Record the total number of statements in the input proof document statements.
	// Let this be known as the total statements.
	statements := canonicalizedLDToStatements(*inputProofDocumentStatements)
	totalStatements := len(statements)

	// 3. Apply the framing algorithm to the input proof document.
	// Let the product of the framing algorithm be known as the revealed document.
	revealedDocument, err := LDFrame(inputProofDocument, revealDocument)
	if err != nil {
		return nil, err
	}

	// 4. Canonicalize the revealed document using the canonicalization algorithm to obtain the set of statements
	// represented as n-quads. Let these be known as the revealed statements.
	marshaledRevealedDocument, err := b.Marshal(revealedDocument)
	if err != nil {
		return nil, err
	}
	canonicalRevealedStatements, err := b.Canonicalize(marshaledRevealedDocument)
	if err != nil {
		return nil, err
	}
	revealedStatements := canonicalizedLDToStatements(*canonicalRevealedStatements)

	// 5. Initialize an empty array of length equal to the number of revealed statements.
	// Let this be known as the revealed indicies array.
	revealedIndicies := make([]int, len(revealedStatements))

	// 6. For each statement in order:
	// 6.1 Find the numerical index the statement occupies in the set input proof document statements.
	// 6.2. Insert this numerical index into the revealed indicies array

	// create an index of all statements in the original doc
	documentStatementsMap := make(map[string]int, totalStatements)
	for i, statement := range statements {
		documentStatementsMap[statement] = i
	}

	// find index of each revealed statement in the original doc
	for i := range revealedStatements {
		statement := revealedStatements[i]
		statementIndex := documentStatementsMap[statement]
		revealedIndicies[i] = statementIndex
	}

	return &DeriveProofResult{
		RevealedIndicies:             revealedIndicies,
		InputProofDocumentStatements: statements,
		RevealedDocument:             revealedDocument.(map[string]any),
	}, nil
}

// Verify verifies a BBS Plus derived proof. Note that the underlying value for `v` must be of type `*BBSPlusVerifier`. Bug here: https://github.com/w3c-ccg/ldp-bbs2020/issues/62
func (b BBSPlusSignatureProofSuite) Verify(v cryptosuite.Verifier, p cryptosuite.WithEmbeddedProof) error {
	proof := p.GetProof()
	gotProof, err := BBSPlusProofFromGenericProof(*proof)
	if err != nil {
		return errors.Wrap(err, "coercing proof into BBSPlusSignature2020Proof proof")
	}

	// remove proof before verifying
	p.SetProof(nil)

	// make sure we set it back after we're done verifying
	defer p.SetProof(proof)

	// remove the proof value in the proof before verification
	signatureValue, err := decodeProofValue(gotProof.ProofValue)
	if err != nil {
		return errors.Wrap(err, "decoding proof value")
	}
	gotProof.SetProofValue("")

	// prepare proof options
	contexts, err := cryptosuite.GetContextsFromProvable(p)
	if err != nil {
		return errors.Wrap(err, "getting contexts from provable")
	}

	// make sure the suite's context(s) are included
	contexts = cryptosuite.EnsureRequiredContexts(contexts, b.RequiredContexts())
	opts := &cryptosuite.ProofOptions{Contexts: contexts}

	// run the create verify hash algorithm on both provable and the proof
	var genericProvable map[string]any
	pBytes, err := json.Marshal(p)
	if err != nil {
		return errors.Wrap(err, "marshaling provable")
	}
	if err = json.Unmarshal(pBytes, &genericProvable); err != nil {
		return errors.Wrap(err, "unmarshaling provable")
	}
	tbv, err := b.CreateVerifyHash(genericProvable, gotProof, opts)
	if err != nil {
		return errors.Wrap(err, "running create verify hash algorithm")
	}

	bbsPlusVerifier, ok := v.(*BBSPlusVerifier)
	if !ok {
		return errors.New("verifier does not implement BBSPlusVerifier")
	}

	nonce, err := base64.StdEncoding.DecodeString(gotProof.Nonce)
	if err != nil {
		return errors.Wrap(err, "decoding nonce")
	}
	if err = bbsPlusVerifier.VerifyDerived(tbv, signatureValue, nonce); err != nil {
		return errors.Wrap(err, "verifying BBS+ signature")
	}
	return nil
}

// CryptoSuiteProofType interface

var _ cryptosuite.CryptoSuiteProofType = (*BBSPlusSignatureProofSuite)(nil)

func (BBSPlusSignatureProofSuite) Marshal(data any) ([]byte, error) {
	// JSONify the provable object
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return jsonBytes, nil
}

func (BBSPlusSignatureProofSuite) Canonicalize(marshaled []byte) (*string, error) {
	// the LD library anticipates a generic golang json object to normalize
	var generic map[string]any
	if err := json.Unmarshal(marshaled, &generic); err != nil {
		return nil, err
	}
	normalized, err := LDNormalize(generic)
	if err != nil {
		return nil, errors.Wrap(err, "canonicalizing provable document")
	}
	canonicalString := normalized.(string)
	return &canonicalString, nil
}

func canonicalizedLDToStatements(canonicalized string) []string {
	lines := strings.Split(canonicalized, "\n")
	res := make([]string, 0, len(lines))
	for i := range lines {
		if strings.TrimSpace(lines[i]) != "" {
			res = append(res, lines[i])
		}
	}
	return res
}

// CreateVerifyHash https://w3c-ccg.github.io/data-integrity-spec/#create-verify-hash-algorithm
// augmented by https://w3c-ccg.github.io/ldp-bbs2020/#create-verify-data-algorithm
func (b BBSPlusSignatureProofSuite) CreateVerifyHash(doc map[string]any, proof crypto.Proof, opts *cryptosuite.ProofOptions) ([]byte, error) {
	// first, make sure "created" exists in the proof and insert an LD context property for the proof vocabulary
	preparedProof, err := b.prepareProof(proof, opts)
	if err != nil {
		return nil, errors.Wrap(err, "preparing proof for the create verify hash algorithm")
	}

	// marshal doc to prepare for canonicalizaiton
	marshaledProvable, err := b.Marshal(doc)
	if err != nil {
		return nil, errors.Wrap(err, "marshalling doc")
	}

	// canonicalize doc using the suite's method
	canonicalProvable, err := b.Canonicalize(marshaledProvable)
	if err != nil {
		return nil, errors.Wrap(err, "canonicalizing doc")
	}

	// marshal proof to prepare for canonicalizaiton
	marshaledOptions, err := b.Marshal(preparedProof)
	if err != nil {
		return nil, errors.Wrap(err, "marshalling proof")
	}

	// 4.1 canonicalize  proof using the suite's method
	canonicalizedOptions, err := b.Canonicalize(marshaledOptions)
	if err != nil {
		return nil, errors.Wrap(err, "canonicalizing proof")
	}

	// 4.2 set output to the result of the hash of the canonicalized options document
	canonicalizedOptionsBytes := []byte(*canonicalizedOptions)
	optionsDigest, err := b.Digest(canonicalizedOptionsBytes)
	if err != nil {
		return nil, errors.Wrap(err, "taking digest of proof")
	}

	// 4.3 hash the canonicalized doc and append it to the output
	canonicalDoc := []byte(*canonicalProvable)
	documentDigest, err := b.Digest(canonicalDoc)
	if err != nil {
		return nil, errors.Wrap(err, "taking digest of doc")
	}

	// 5. return the output
	output := append(optionsDigest, documentDigest...)
	return output, nil
}

func (b BBSPlusSignatureProofSuite) prepareProof(proof crypto.Proof, opts *cryptosuite.ProofOptions) (*crypto.Proof, error) {
	proofBytes, err := json.Marshal(proof)
	if err != nil {
		return nil, err
	}

	var genericProof map[string]any
	if err = json.Unmarshal(proofBytes, &genericProof); err != nil {
		return nil, err
	}

	// must make sure the proof does not have a proof value or nonce before signing/verifying
	delete(genericProof, "proofValue")
	delete(genericProof, "nonce")

	// make sure the proof has a timestamp
	created, ok := genericProof["created"]
	if !ok || created == "" {
		genericProof["created"] = GetRFC3339Timestamp()
	}

	// for verification, we must replace the BBS ProofType with the Signature Type
	genericProof["type"] = BBSPlusSignature2020

	var contexts []any
	if opts != nil {
		contexts = opts.Contexts
	} else {
		// if none provided, make sure the proof has a context value for this suite
		contexts = ArrayStrToInterface(b.RequiredContexts())
	}
	genericProof["@context"] = contexts
	p := crypto.Proof(genericProof)
	return &p, nil
}

func (BBSPlusSignatureProofSuite) Digest(tbd []byte) ([]byte, error) {
	// handled by the algorithm itself
	return tbd, nil
}
