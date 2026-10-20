NIMBLE_VERSION := 0.26.0
ZEROKIT_COMMIT := ea80f39be3e7944e4537b5f4726a7c4aabfe0ab5
RLN_VERSION := v2.0.2

NIMBLE_BIN := $(HOME)/.local/nimble-$(NIMBLE_VERSION)/bin
export PATH := $(NIMBLE_BIN):$(PATH)
unexport NIMBLE_DIR

NIM_VERSION := $(shell sed -n 's/^requires "nim == \([0-9.]*\)".*/\1/p' lossmon_client.nimble)
LOSSMON_NIM ?= $(firstword $(shell ls -d $(CURDIR)/nimbledeps/pkgs2/nim-$(NIM_VERSION)-*/bin/nim 2>/dev/null))
LD_VERSION ?= v$(shell ls -d nimbledeps/pkgs2/logos_delivery-* 2>/dev/null | sed 's/.*logos_delivery-\([0-9.]*\)-.*/\1/')
RLN_FILE := $(CURDIR)/build/librln_$(RLN_VERSION).a
LOG_LEVEL ?= NOTICE
LINK_FLAGS ?=
NIM_TEST_FLAGS := --mm:refc --hints:off --verbosity:0
DEPS_STAMP := nimbledeps/.nimble-setup

NIM_FLAGS = --mm:refc -d:release -d:chronicles_log_level=$(LOG_LEVEL) \
	-d:libp2p_mix_experimental_exit_is_dest -d:libp2p_quic_support \
	-d:git_version=$(LD_VERSION) -d:lossmon_ld_version=$(LD_VERSION) \
	--passL:$(RLN_FILE) --passL:-lm $(LINK_FLAGS)

.PHONY: lossmon-client deps test clean distclean

lossmon-client: deps
	mkdir -p build
	$(LOSSMON_NIM) c $(NIM_FLAGS) --out:build/lossmon-client src/lossmon_client.nim

deps: $(DEPS_STAMP) $(RLN_FILE)

$(NIMBLE_BIN)/nimble:
	scripts/install-nimble.sh $(NIMBLE_VERSION) $(NIMBLE_BIN)

$(DEPS_STAMP): lossmon_client.nimble nimble.lock | $(NIMBLE_BIN)/nimble
	nimble setup --localdeps -y
	scripts/native-deps.sh nimbledeps
	touch $@

$(RLN_FILE):
	mkdir -p build
	scripts/build-librln.sh $(ZEROKIT_COMMIT) $@

clean:
	rm -rf build/lossmon-client nimcache

distclean: clean
	rm -rf build nimbledeps nimble.paths

TESTS := $(basename $(notdir $(wildcard tests/test_*.nim)))

test: deps
	mkdir -p build
	@for t in $(TESTS); do \
		$(LOSSMON_NIM) c -r $(NIM_TEST_FLAGS) --out:build/$$t tests/$$t.nim || exit 1; \
	done
