#pragma once
#include <string>
#include <spdlog/spdlog.h>

namespace ts::vms::media::utils {

class Logger {
public:
    static void Init(const std::string& log_level);
    static std::string RedactRtspUrl(const std::string& url);
};

} // namespace ts::vms::media::utils
