#include "logger.hpp"
#include <spdlog/sinks/stdout_color_sinks.h>
#include <regex>

namespace ts::vms::media::utils {

void Logger::Init(const std::string& log_level) {
    auto console = spdlog::stdout_color_mt("console");
    spdlog::set_default_logger(console);
    
    if (log_level == "debug") spdlog::set_level(spdlog::level::debug);
    else if (log_level == "info") spdlog::set_level(spdlog::level::info);
    else if (log_level == "warn") spdlog::set_level(spdlog::level::warn);
    else if (log_level == "error") spdlog::set_level(spdlog::level::err);
    else spdlog::set_level(spdlog::level::info);

    spdlog::set_pattern("[%Y-%m-%d %H:%M:%S.%e] [%^%l%$] %v");
}

std::string Logger::RedactRtspUrl(const std::string& url) {
    auto pos_at = url.find('@');
    if (pos_at == std::string::npos) return url;
    
    auto pos_prot = url.find("://");
    if (pos_prot == std::string::npos || pos_prot > pos_at) return url;
    
    std::string prot = url.substr(0, pos_prot);
    if (prot != "rtsp" && prot != "rtsps") return url;

    return prot + "://***:***" + url.substr(pos_at);
}

} // namespace ts::vms::media::utils
