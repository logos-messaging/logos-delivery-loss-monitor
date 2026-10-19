VENDOR := vendor/logos-delivery
NIM_VERSION := $(shell sed -n 's/^requires "nim == \([0-9.]*\)".*/\1/p' $(VENDOR)/logos_delivery.nimble)
LOSSMON_NIM ?= $(firstword $(wildcard $(abspath $(VENDOR))/nimbledeps/pkgs2/nim-$(NIM_VERSION)-*/bin/nim) $(HOME)/.nim/nim-$(NIM_VERSION)/bin/nim)
LD_VERSION := $(shell git -C $(VENDOR) describe --tags --always 2>/dev/null)
RLN_VERSION := $(shell sed -n 's/^LIBRLN_VERSION := //p' $(VENDOR)/Makefile)
RLN_FILE := $(abspath $(VENDOR))/librln_$(RLN_VERSION).a
LOG_LEVEL ?= NOTICE
NIM_TEST_FLAGS := --mm:refc --hints:off --verbosity:0
DEPS_STAMP := $(VENDOR)/nimbledeps/.nimble-setup

NIM_FLAGS := --mm:refc -d:release -d:chronicles_log_level=$(LOG_LEVEL) \
	-d:libp2p_mix_experimental_exit_is_dest -d:libp2p_quic_support \
	-d:git_version=$(LD_VERSION) -d:lossmon_ld_version=$(LD_VERSION) \
	--passL:$(RLN_FILE) --passL:-lm

.PHONY: lossmon-client deps test clean

lossmon-client: deps
	mkdir -p build
	$(LOSSMON_NIM) c $(NIM_FLAGS) --out:build/lossmon-client src/lossmon_client.nim

deps: $(DEPS_STAMP) $(RLN_FILE)

$(DEPS_STAMP):
	test -f $(VENDOR)/logos_delivery.nimble || git submodule update --init --recursive $(VENDOR)
	$(MAKE) -C $(VENDOR) build-deps

$(RLN_FILE): | $(DEPS_STAMP)
	$(MAKE) -C $(VENDOR) librln

clean:
	rm -rf build nimcache

TESTS := $(basename $(notdir $(wildcard tests/test_*.nim)))

test: deps
	mkdir -p build
	@for t in $(TESTS); do \
		$(LOSSMON_NIM) c -r $(NIM_TEST_FLAGS) --out:build/$$t tests/$$t.nim || exit 1; \
	done
