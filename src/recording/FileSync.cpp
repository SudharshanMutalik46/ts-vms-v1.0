#include "FileSync.h"

#include <array>
#include <cstdint>
#include <fstream>
#include <iomanip>
#include <sstream>
#include <vector>

#ifdef _WIN32
#include <windows.h>
#else
#include <fcntl.h>
#include <unistd.h>
#endif

namespace {

class Sha256 final {
public:
    Sha256() { Reset(); }

    void Reset() {
        data_len_ = 0;
        bit_len_ = 0;
        state_[0] = 0x6a09e667U;
        state_[1] = 0xbb67ae85U;
        state_[2] = 0x3c6ef372U;
        state_[3] = 0xa54ff53aU;
        state_[4] = 0x510e527fU;
        state_[5] = 0x9b05688cU;
        state_[6] = 0x1f83d9abU;
        state_[7] = 0x5be0cd19U;
    }

    void Update(const uint8_t* data, size_t len) {
        for (size_t i = 0; i < len; ++i) {
            data_[data_len_++] = data[i];
            if (data_len_ == 64) {
                Transform();
                bit_len_ += 512;
                data_len_ = 0;
            }
        }
    }

    std::array<uint8_t, 32> Final() {
        std::array<uint8_t, 32> hash{};

        uint32_t i = data_len_;

        if (data_len_ < 56) {
            data_[i++] = 0x80;
            while (i < 56) data_[i++] = 0x00;
        } else {
            data_[i++] = 0x80;
            while (i < 64) data_[i++] = 0x00;
            Transform();
            std::fill(data_.begin(), data_.begin() + 56, 0);
        }

        bit_len_ += static_cast<uint64_t>(data_len_) * 8ULL;
        data_[63] = static_cast<uint8_t>(bit_len_);
        data_[62] = static_cast<uint8_t>(bit_len_ >> 8);
        data_[61] = static_cast<uint8_t>(bit_len_ >> 16);
        data_[60] = static_cast<uint8_t>(bit_len_ >> 24);
        data_[59] = static_cast<uint8_t>(bit_len_ >> 32);
        data_[58] = static_cast<uint8_t>(bit_len_ >> 40);
        data_[57] = static_cast<uint8_t>(bit_len_ >> 48);
        data_[56] = static_cast<uint8_t>(bit_len_ >> 56);
        Transform();

        for (i = 0; i < 4; ++i) {
            hash[i]      = static_cast<uint8_t>((state_[0] >> (24 - i * 8)) & 0xff);
            hash[i + 4]  = static_cast<uint8_t>((state_[1] >> (24 - i * 8)) & 0xff);
            hash[i + 8]  = static_cast<uint8_t>((state_[2] >> (24 - i * 8)) & 0xff);
            hash[i + 12] = static_cast<uint8_t>((state_[3] >> (24 - i * 8)) & 0xff);
            hash[i + 16] = static_cast<uint8_t>((state_[4] >> (24 - i * 8)) & 0xff);
            hash[i + 20] = static_cast<uint8_t>((state_[5] >> (24 - i * 8)) & 0xff);
            hash[i + 24] = static_cast<uint8_t>((state_[6] >> (24 - i * 8)) & 0xff);
            hash[i + 28] = static_cast<uint8_t>((state_[7] >> (24 - i * 8)) & 0xff);
        }

        return hash;
    }

private:
    static constexpr std::array<uint32_t, 64> kTable_ = {
        0x428a2f98U,0x71374491U,0xb5c0fbcfU,0xe9b5dba5U,0x3956c25bU,0x59f111f1U,0x923f82a4U,0xab1c5ed5U,
        0xd807aa98U,0x12835b01U,0x243185beU,0x550c7dc3U,0x72be5d74U,0x80deb1feU,0x9bdc06a7U,0xc19bf174U,
        0xe49b69c1U,0xefbe4786U,0x0fc19dc6U,0x240ca1ccU,0x2de92c6fU,0x4a7484aaU,0x5cb0a9dcU,0x76f988daU,
        0x983e5152U,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd5a79147U,0x06ca6351U,0x14292967U,
        0x27b70a85U,0x2e1b2138U,0x4d2c6dfcU,0x53380d13U,0x650a7354U,0x766a0abbU,0x81c2c92eU,0x92722c85U,
        0xa2bfe8a1U,0xa81a664bU,0xc24b8b70U,0xc76c51a3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,
        0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5b9cca4fU,0x682e6ff3U,
        0x748f82eeU,0x78a5636fU,0x84c87814U,0x8cc70208U,0x90befffaU,0xa4506cebU,0xbef9a3f7U,0xc67178f2U
    };

    static uint32_t RotR(uint32_t x, uint32_t n) { return (x >> n) | (x << (32 - n)); }
    static uint32_t Ch(uint32_t x, uint32_t y, uint32_t z) { return (x & y) ^ (~x & z); }
    static uint32_t Maj(uint32_t x, uint32_t y, uint32_t z) { return (x & y) ^ (x & z) ^ (y & z); }
    static uint32_t Sig0(uint32_t x) { return RotR(x, 2) ^ RotR(x, 13) ^ RotR(x, 22); }
    static uint32_t Sig1(uint32_t x) { return RotR(x, 6) ^ RotR(x, 11) ^ RotR(x, 25); }
    static uint32_t Theta0(uint32_t x) { return RotR(x, 7) ^ RotR(x, 18) ^ (x >> 3); }
    static uint32_t Theta1(uint32_t x) { return RotR(x, 17) ^ RotR(x, 19) ^ (x >> 10); }

    void Transform() {
        uint32_t m[64];
        for (uint32_t i = 0, j = 0; i < 16; ++i, j += 4) {
            m[i] = (static_cast<uint32_t>(data_[j]) << 24) |
                   (static_cast<uint32_t>(data_[j + 1]) << 16) |
                   (static_cast<uint32_t>(data_[j + 2]) << 8) |
                   (static_cast<uint32_t>(data_[j + 3]));
        }
        for (uint32_t i = 16; i < 64; ++i) {
            m[i] = Theta1(m[i - 2]) + m[i - 7] + Theta0(m[i - 15]) + m[i - 16];
        }

        uint32_t a = state_[0];
        uint32_t b = state_[1];
        uint32_t c = state_[2];
        uint32_t d = state_[3];
        uint32_t e = state_[4];
        uint32_t f = state_[5];
        uint32_t g = state_[6];
        uint32_t h = state_[7];

        for (uint32_t i = 0; i < 64; ++i) {
            uint32_t t1 = h + Sig1(e) + Ch(e, f, g) + kTable_[i] + m[i];
            uint32_t t2 = Sig0(a) + Maj(a, b, c);
            h = g;
            g = f;
            f = e;
            e = d + t1;
            d = c;
            c = b;
            b = a;
            a = t1 + t2;
        }

        state_[0] += a;
        state_[1] += b;
        state_[2] += c;
        state_[3] += d;
        state_[4] += e;
        state_[5] += f;
        state_[6] += g;
        state_[7] += h;
    }

    std::array<uint8_t, 64> data_{};
    uint32_t data_len_ = 0;
    uint64_t bit_len_ = 0;
    uint32_t state_[8]{};
};

std::string ToHex(const std::array<uint8_t, 32>& bytes) {
    std::ostringstream out;
    out << std::hex << std::setfill('0');
    for (uint8_t b : bytes) {
        out << std::setw(2) << static_cast<unsigned>(b);
    }
    return out.str();
}

} // namespace

namespace ts {
namespace vms {
namespace recording {

bool FileSync::FlushToDisk(const std::string& filepath) {
#ifdef _WIN32
    HANDLE hFile = CreateFileA(
        filepath.c_str(),
        GENERIC_WRITE,
        FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
        nullptr,
        OPEN_EXISTING,
        FILE_ATTRIBUTE_NORMAL,
        nullptr);

    if (hFile == INVALID_HANDLE_VALUE) {
        return false;
    }

    const bool ok = FlushFileBuffers(hFile) != 0;
    CloseHandle(hFile);
    return ok;
#else
    int fd = open(filepath.c_str(), O_WRONLY);
    if (fd < 0) {
        return false;
    }

    const bool ok = (fsync(fd) == 0);
    close(fd);
    return ok;
#endif
}

std::string FileSync::ComputeChecksum(const std::string& filepath) {
    std::ifstream file(filepath, std::ios::binary);
    if (!file.is_open()) {
        return {};
    }

    Sha256 sha;
    std::vector<uint8_t> buffer(1024 * 1024);

    while (file.good()) {
        file.read(reinterpret_cast<char*>(buffer.data()), static_cast<std::streamsize>(buffer.size()));
        const std::streamsize read = file.gcount();
        if (read > 0) {
            sha.Update(buffer.data(), static_cast<size_t>(read));
        }
    }

    if (file.bad()) {
        return {};
    }

    return ToHex(sha.Final());
}

}
}
}
