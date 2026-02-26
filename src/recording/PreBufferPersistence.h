#pragma once
#include <string>
#include <iostream>

namespace ts {
namespace vms {
namespace recording {

// Forward declare Frame
struct BufferedFrame;

class IPreBufferPersistence {
public:
    virtual ~IPreBufferPersistence() = default;
    virtual void SaveFrame(const std::string& camera_id, const BufferedFrame& frame) = 0;
    virtual void Restore(const std::string& camera_id) = 0;
};

// Phase 4.6 Stub
class StubPreBufferPersistence : public IPreBufferPersistence {
public:
    void SaveFrame(const std::string& camera_id, const BufferedFrame& frame) override {
        // No-op for stub
    }
    void Restore(const std::string& camera_id) override {
        std::cout << "[PreBuffer] Persistence disabled. No frames restored for " << camera_id << ".\n";
    }
};

}
}
}
