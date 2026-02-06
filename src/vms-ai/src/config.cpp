#include "config.h"
#include <cstdlib>
#include <iostream>

namespace vms_ai {

Config Config::LoadFromEnv() {
    Config c;
    
    if (const char* env = std::getenv("NATS_URL"))
        c.nats_url = env;
    if (const char* env = std::getenv("CONTROL_PLANE_URL"))
        c.control_plane_url = env;
    if (const char* env = std::getenv("AI_SERVICE_TOKEN"))
        c.ai_service_token = env;
        
    if (const char* env = std::getenv("MAX_CAMERAS"))
        c.max_cameras = std::stoi(env);
        
    if (const char* env = std::getenv("ENABLE_WEAPON_AI"))
        c.enable_weapon_ai = (std::string(env) == "true");

    // Override model paths if env vars set (optional, defaults usually fine)
    if (const char* env = std::getenv("MODEL_BASIC_PATH"))
        c.model_basic_path = env;
    if (const char* env = std::getenv("MODEL_WEAPON_PATH"))
        c.model_weapon_path = env;

    std::cout << "[AI Service] Loaded Config: NATS=" << c.nats_url 
              << ", CP=" << c.control_plane_url 
              << ", MaxCams=" << c.max_cameras 
              << ", WeaponAI=" << (c.enable_weapon_ai ? "ON" : "OFF") << std::endl;
              
    return c;
}

} // namespace vms_ai
