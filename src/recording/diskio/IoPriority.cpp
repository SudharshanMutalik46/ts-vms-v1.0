#include "IoPriority.h"
#include <iostream>

namespace ts {
namespace vms {
namespace diskio {

void IoPriority::SetBackgroundPriority() {
    if (!SetThreadPriority(GetCurrentThread(), THREAD_MODE_BACKGROUND_BEGIN)) {
        std::cerr << "[IoPriority] Failed to set background priority. Error: " << GetLastError() << "\n";
    }
}

void IoPriority::RevertNormalPriority() {
    if (!SetThreadPriority(GetCurrentThread(), THREAD_MODE_BACKGROUND_END)) {
        std::cerr << "[IoPriority] Failed to revert normal priority. Error: " << GetLastError() << "\n";
    }
}

} // namespace diskio
} // namespace vms
} // namespace ts
