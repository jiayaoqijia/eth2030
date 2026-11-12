export const roadmapPhases = [
  { name: 'Glamsterdam', highlights: ['ePBS', 'Block-level access lists', 'Gas repricing'], note: 'Sepolia activated; Hoodi postponed. Mainnet date has not been announced.' },
  { name: 'Hegotá', highlights: ['FOCIL', 'Quick slots', 'Frame transactions', 'PQ key registrations'], note: 'Scope and timing remain subject to the upgrade process.' },
  { name: 'I*', highlights: ['Decoupled consensus', 'leanSPHINCS transactions', 'leanVM PQ proofs'], note: 'Placeholder name; proposed work.' },
  { name: 'J*', highlights: ['leanXMSS PQ attestations', 'leanDA PQ sampling', 'leanPCS frames'], note: 'Placeholder name; proposed work.' },
  { name: 'K*', highlights: ['Mandatory 1-of-1 proofs', 'Decentralized state'], note: 'Placeholder name; proposed work.' },
  { name: 'L*', highlights: ['1-round finality', 'Native rollups'], note: 'Placeholder name; proposed work.' },
  { name: 'Longer term', highlights: ['Shielded transfers', 'Encrypted mempool', 'Distributed block building'], note: 'Research directions, not scheduled releases.' },
];

export const evidence = [
  { title: 'Block-level access lists', spec: 'EIP-7928', eip: '7928', code: 'pkg/bal', test: 'pkg/bal/spec_test.go', summary: 'Access tracking, ordering validation and scheduling code with package tests.', limit: 'Full current-spec conformance and network interoperability have not been established by this review.' },
  { title: 'Enshrined proposer-builder separation', spec: 'EIP-7732', eip: '7732', code: 'pkg/epbs', test: 'pkg/epbs/validation_test.go', summary: 'Builder bid and payload validation, payment state and Engine API code.', limit: 'Bid validation permits zero signatures or public keys in its testing path; end-to-end consensus interoperability is not established here.' },
  { title: 'Inclusion lists (FOCIL)', spec: 'EIP-7805', eip: '7805', code: 'pkg/focil', test: 'pkg/focil/il_satisfaction_test.go', summary: 'Inclusion-list construction and satisfaction checks, including gas and state exemptions.', limit: 'Some checks are simplified; fork-choice and cross-client conformance require further verification.' },
  { title: 'Frame transactions', spec: 'EIP-8141', eip: '8141', code: 'pkg/core/eips/frame_execution.go', test: 'pkg/core/eips/frame_execution_test.go', summary: 'Frame execution and approval logic with focused tests.', limit: 'The EIP is a changing draft; full current-spec conformance and interoperability remain to be assessed.' },
  { title: 'Native rollups', spec: 'EIP-8079', eip: '8079', code: 'pkg/rollup/execute', test: 'pkg/rollup/execute_test.go', summary: 'Experimental EXECUTE-related code and tests.', limit: 'This is not evidence of a complete, production-ready implementation of the draft.' },
];

export const gettingStartedCommands = [
  { comment: '# Clone the repository', command: 'git clone https://github.com/alt-research/eth2030.git' },
  { comment: '# Build and test the Go module', command: 'cd eth2030/pkg && go build ./... && go test ./...' },
  { comment: '# Build the experimental node', command: 'go build -o eth2030-geth ./cmd/eth2030-geth/' },
];
