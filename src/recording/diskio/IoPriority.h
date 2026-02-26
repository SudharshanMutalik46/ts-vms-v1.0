#pragma once
#include <windows.h>

namespace ts {
namespace vms {
namespace diskio {

class IoPriority {
public:
    static void SetBackgroundPriority();
    static void RevertNormalPriority();
};

} // namespace diskio
} // namespace vms
} // namespace ts
