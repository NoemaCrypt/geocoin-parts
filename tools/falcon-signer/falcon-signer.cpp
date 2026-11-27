// falcon-signer — офлайн-подпись sidechain-транзакций GeoCoin.
//
// Режимы:
//   falcon-signer payload <privkey_hex32> <payload_hex|->
//       Подпись «recoverable message» Falcon-512 над SHA-256(payload) —
//       то, что ожидают finalize... методы (sidechain_signature).
//   falcon-signer tx <privkey_hex32> <tx_hex|->
//       Полная обработка транзакции: подпись всех входов (witness),
//       sidechain_signature над payload для подписи, сериализация.
//       Требует библиотеки core (linkgeocoin_core), исходники L1 не меняются.
#include <cstdio>
#include <string>
#include <vector>

#include <geocoin/core/transaction.hpp>
#include <geocoin/crypto/geo_area.hpp>
#include <geocoin/crypto/signatures.hpp>
#include <geocoin/types.hpp>
#include <geocoin/utils/bytes.hpp>

using namespace geocoin;

static bool read_hex_or_stdin(const std::string& hex_or_dash, bytes& out) {
    if (hex_or_dash == "-") {
        char buf[65536];
        size_t n;
        while ((n = std::fread(buf, 1, sizeof(buf), stdin)) > 0) {
            out.insert(out.end(), buf, buf + n);
        }
        // stdin содержит hex-текст
        bytes tmp;
        if (!utils::hex_to_bytes_checked(std::string(out.begin(), out.end()), tmp)) {
            return false;
        }
        out = std::move(tmp);
        return true;
    }
    return utils::hex_to_bytes_checked(hex_or_dash, out);
}

static void print_hex(const bytes& data) {
    static const char* digits = "0123456789abcdef";
    for (byte b : data) {
        std::putchar(digits[b >> 4]);
        std::putchar(digits[b & 0xF]);
    }
    std::putchar('\n');
}

int main(int argc, char** argv) {
    if (argc < 4) {
        std::fprintf(stderr,
            "usage: falcon-signer payload <privkey_hex32> <payload_hex|->\n"
            "       falcon-signer tx <privkey_hex32> <tx_hex|->\n");
        return 2;
    }
    const std::string mode = argv[1];
    const std::string key_hex = argv[2];
    const std::string data_hex = argv[3];

    bytes priv;
    if (!utils::hex_to_bytes_checked(key_hex, priv) || priv.size() != 32) {
        std::fprintf(stderr, "privkey должен быть 32 байта hex\n");
        return 2;
    }

    if (mode == "payload") {
        bytes payload;
        if (!read_hex_or_stdin(data_hex, payload)) {
            std::fprintf(stderr, "payload_hex invalid\n");
            return 2;
        }
        const Hash256 msg32 = crypto::sha256(payload);
        const Signature sig = crypto::sign_recoverable_message(msg32, priv);
        Address recovered;
        if (!crypto::recover_signer_address_from_signed_message(msg32, sig, recovered)) {
            std::fprintf(stderr, "self-check failed\n");
            return 3;
        }
        print_hex(sig);
        return 0;
    }

    if (mode == "roundtrip") {
        bytes tx_data;
        if (!read_hex_or_stdin(data_hex, tx_data)) { std::fprintf(stderr, "tx_hex invalid\n"); return 2; }
        core::Transaction tx;
        size_t off = 0;
        try { tx = core::Transaction::deserialize(tx_data, off); }
        catch (const std::exception& e) { std::fprintf(stderr, "deserialize failed: %s\n", e.what()); return 3; }
        if (off != tx_data.size()) { std::fprintf(stderr, "consumed %zu of %zu\n", off, tx_data.size()); }
        bytes re = tx.serialize();
        std::fprintf(stderr, "in=%zu out=%zu equal=%d\n", tx_data.size(), re.size(), re == tx_data ? 1 : 0);
        if (re != tx_data) {
            for (size_t i = 0; i < (tx_data.size() < re.size() ? tx_data.size() : re.size()); ++i) {
                if (tx_data[i] != re[i]) { std::fprintf(stderr, "first diff at %zu: %02x vs %02x\n", i, tx_data[i], re[i]); break; }
            }
        }
        print_hex(re);
        return 0;
    }

    if (mode == "addr") {
        Address out;
        if (!crypto::derive_identity_address_from_private_key(priv, out)) { std::fprintf(stderr, "derive failed\n"); return 3; }
        std::printf("%s\n", out.str().c_str());
        return 0;
    }

    if (mode == "tx") {
        bytes tx_data;
        if (!read_hex_or_stdin(data_hex, tx_data)) {
            std::fprintf(stderr, "tx_hex invalid\n");
            return 2;
        }
        core::Transaction tx;
        size_t off = 0;
        try {
            tx = core::Transaction::deserialize(tx_data, off);
        } catch (const std::exception& e) {
            std::fprintf(stderr, "tx deserialize failed: %s\n", e.what());
            return 3;
        }
        if (off != tx_data.size()) {
            std::fprintf(stderr, "tx deserialize: %zu из %zu байт\n", off, tx_data.size());
            return 3;
        }
        if (tx.sidechain_owner.str().empty()) {
            std::fprintf(stderr, "tx не содержит sidechain_owner\n");
            return 4;
        }
        // 1) sidechain_signature над payload для подписи (payload не зависит
        //    от самого sidechain_signature, поэтому можно подписывать первым)
        const bytes payload = core::build_sidechain_payload_for_signing(tx);
        const Hash256 msg32 = crypto::sha256(payload);
        tx.sidechain_signature = crypto::sign_recoverable_message(msg32, priv);
        Address recovered;
        if (!crypto::recover_signer_address_from_signed_message(msg32, tx.sidechain_signature, recovered)) {
            std::fprintf(stderr, "payload self-check failed\n");
            return 3;
        }
        // 2) witness для входов — ПОСЛЕ вставки sidechain_signature:
        //    hash входа = double_sha256(serialize_without_witness), и он уже
        //    включает sidechain_signature
        tx.sign(priv);
        if (!tx.verify()) {
            std::fprintf(stderr, "tx.verify() failed\n");
            return 4;
        }
        print_hex(tx.serialize());
        return 0;
    }

    std::fprintf(stderr, "unknown mode: %s\n", mode.c_str());
    return 2;
}
