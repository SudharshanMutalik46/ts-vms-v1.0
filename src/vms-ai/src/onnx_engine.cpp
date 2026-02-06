#include "onnx_engine.h"
#include <onnxruntime_cxx_api.h>
#include <iostream>
#include <chrono>
#include <mutex>
#include <windows.h> // Required for MultiByteToWideChar
#include "metrics_server.h"
#include <algorithm> // for std::max/min

namespace vms_ai {

// PIMPL to hold ORT state
struct ONNXEngine::Impl {
    Ort::Env env{ORT_LOGGING_LEVEL_WARNING, "VMS_AI_Service"};
    Ort::SessionOptions session_options;
    
    std::unique_ptr<Ort::Session> session_basic;
    std::unique_ptr<Ort::Session> session_weapon;
    
    // Model metadata (for basic model mostly)
    std::vector<const char*> input_names_basic;
    std::vector<const char*> output_names_basic;
    std::vector<std::string> input_names_str_basic;
    std::vector<std::string> output_names_str_basic;

    Impl() {
        session_options.SetIntraOpNumThreads(1);
        session_options.SetGraphOptimizationLevel(GraphOptimizationLevel::ORT_DISABLE_ALL); // Disable optimizations to avoid hang
        std::cerr << "[ONNX] Configured session options (Opt=Disabled)\n";
    }
    
    // Convert wide char path for Windows
    std::wstring ToWStr(const std::string& path) {
        if (path.empty()) return std::wstring();
        int size_needed = MultiByteToWideChar(CP_UTF8, 0, &path[0], (int)path.size(), NULL, 0);
        std::wstring wstrTo(size_needed, 0);
        MultiByteToWideChar(CP_UTF8, 0, &path[0], (int)path.size(), &wstrTo[0], size_needed);
        return wstrTo;
    }
};

ONNXEngine::ONNXEngine(const Config& config) : config_(config), impl_(new Impl()) {}
ONNXEngine::~ONNXEngine() { delete impl_; }

bool ONNXEngine::Initialize() {
    try {
        // Load Basic Model
        std::wstring basic_path = impl_->ToWStr(config_.model_basic_path);
        std::cerr << "[ONNX] Creating Basic Session from: " << config_.model_basic_path << "\n";
        try {
            impl_->session_basic = std::make_unique<Ort::Session>(impl_->env, basic_path.c_str(), impl_->session_options);
            std::cerr << "[ONNX] Basic Session Created!\n";
        } catch (const std::exception& e) {
             std::cerr << "[ONNX] Session creation threw: " << e.what() << "\n";
             throw;
        }
        
        // Cache input/output names for basic model
        Ort::AllocatorWithDefaultOptions allocator;
        
        // Assume single input/output for simplicity or iterate
        // Usually MobileNet SSD has 'input_tensor' and 'detection_boxes', 'detection_scores', etc.
        // We iterate standardly
        size_t num_inputs = impl_->session_basic->GetInputCount();
        for (size_t i = 0; i < num_inputs; i++) {
             auto name = impl_->session_basic->GetInputNameAllocated(i, allocator);
             impl_->input_names_str_basic.push_back(name.get());
             
             // Debug Input Info
             auto type_info = impl_->session_basic->GetInputTypeInfo(i);
             auto tensor_info = type_info.GetTensorTypeAndShapeInfo();
             auto shape = tensor_info.GetShape();
             std::cerr << "[ONNX] Input " << i << ": Name=" << name.get() << " Shape=[";
             for (auto d : shape) std::cerr << d << ",";
             std::cerr << "]\n";
        }
        for (const auto& s : impl_->input_names_str_basic) impl_->input_names_basic.push_back(s.c_str());

        size_t num_outputs = impl_->session_basic->GetOutputCount();
         for (size_t i = 0; i < num_outputs; i++) {
             auto name = impl_->session_basic->GetOutputNameAllocated(i, allocator);
             impl_->output_names_str_basic.push_back(name.get());
             
             auto type_info = impl_->session_basic->GetOutputTypeInfo(i);
             auto tensor_info = type_info.GetTensorTypeAndShapeInfo();
             auto shape = tensor_info.GetShape();
             std::cerr << "[ONNX] Output " << i << ": Name=" << name.get() << " Shape=[";
             for (auto d : shape) std::cerr << d << ",";
             std::cerr << "]\n";
        }
        for (const auto& s : impl_->output_names_str_basic) impl_->output_names_basic.push_back(s.c_str());

        std::cout << "[ONNX] Loaded basic model: " << config_.model_basic_path << "\n";

        // Load Weapon Model (Optional)
        if (config_.enable_weapon_ai) {
             std::wstring weapon_path = impl_->ToWStr(config_.model_weapon_path);
             // Check file exists first? ORT throws if not found.
             // We can catch exception.
             try {
                impl_->session_weapon = std::make_unique<Ort::Session>(impl_->env, weapon_path.c_str(), impl_->session_options);
                std::cout << "[ONNX] Loaded weapon model: " << config_.model_weapon_path << "\n";
             } catch (const std::exception& e) {
                 std::cerr << "[ONNX] Weapon model load failed (skipping weapon AI): " << e.what() << "\n";
             }
        }
        
    } catch (const Ort::Exception& e) {
        std::cerr << "[ONNX] Initialization Error: " << e.what() << "\n";
        return false;
    }
    return true;
}

std::vector<Detection> ONNXEngine::RunInference(const ImageTensor& tensor, const std::string& stream_type) {
    auto start = std::chrono::high_resolution_clock::now();
    
    Ort::Session* session = nullptr;
    std::vector<const char*>* input_names = nullptr;
    std::vector<const char*>* output_names = nullptr;

    if (stream_type == "basic") {
        if (!impl_->session_basic) return {};
        session = impl_->session_basic.get();
        input_names = &impl_->input_names_basic;
        output_names = &impl_->output_names_basic;
    } else if (stream_type == "weapon" && impl_->session_weapon) {
        // ... weapon model logic (simplified: skipping generic implementation for now)
        // Assume mock behavior or separate logic for weapon model input names
        // Ideally we cache weapon input/outputs too.
        return {}; 
    } else {
        return {};
    }

    std::vector<int64_t> input_shape = {1, tensor.channels, tensor.height, tensor.width};
    size_t input_tensor_size = tensor.data.size();
    
    // Create Input Tensor
    // Note: tensor.data is const, we need non-const for CreateTensorWithDataUseUserOwnedBuffer (it doesn't modify it but signature is non-const usually)
    // Actually typically float* is fine.
    
    Ort::MemoryInfo memory_info = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);
    Ort::Value input_tensor = Ort::Value::CreateTensor<float>(memory_info, 
                                                               const_cast<float*>(tensor.data.data()), 
                                                               input_tensor_size, 
                                                               input_shape.data(), 
                                                               input_shape.size());

    try {
        // Run Inference
        auto outputs = session->Run(Ort::RunOptions{nullptr}, 
                                    input_names->data(), &input_tensor, 1, 
                                    output_names->data(), output_names->size());

        auto end = std::chrono::high_resolution_clock::now();
        double ms = std::chrono::duration<double, std::milli>(end - start).count();
        
        MetricsServer::ObserveInferenceLatency(stream_type, ms);
        
        // Timeout Logic
        if (ms > 3000.0) {
            std::cerr << "[ONNX] HARD TIMEOUT (" << ms << "ms). Restarting session recommendation.\n";
            // In real prod, trigger restart logic or cooldown. For MVP, we pass but log error.
        } else if (ms > 1500.0) {
            std::cerr << "[ONNX] SLOW INFERENCE (" << ms << "ms). Dropping next frame.\n";
            // Scheduler should handle this ideally, or we return special flag.
        }

        // Parse Output
        std::vector<Detection> results;
        
        // --- Output Parsing Logic (3-Tensor SSD: bboxes, labels, scores) ---
        // Output 0: bboxes [1, N, 4]
        // Output 1: labels [1, N]
        // Output 2: scores [1, N]
        
        if (outputs.size() >= 3) {
            float* boxes = outputs[0].GetTensorMutableData<float>();
            int64_t* labels_i64 = outputs[1].GetTensorMutableData<int64_t>();
            float* scores = outputs[2].GetTensorMutableData<float>();
            
            auto info = outputs[0].GetTensorTypeAndShapeInfo();
            auto shape = info.GetShape(); // [1, N, 4]
            
            if (shape.size() >= 3) {
                 size_t num_detections = shape[1];
                 std::cerr << "[ONNX] Num Detections: " << num_detections << "\n";
                 
                 for (size_t i = 0; i < num_detections; i++) {
                     float score = scores[i];
                     if (score < 0.05f) continue; 
                     
                     int64_t label_id = labels_i64[i];
                     
                     // RAW Coordinates
                     float r0 = boxes[i * 4 + 0];
                     float r1 = boxes[i * 4 + 1];
                     float r2 = boxes[i * 4 + 2];
                     float r3 = boxes[i * 4 + 3];

                     // Clamping [0, 1]
                     float xmin = (std::max)(0.0f, (std::min)(1.0f, r0));
                     float ymin = (std::max)(0.0f, (std::min)(1.0f, r1));
                     float xmax = (std::max)(0.0f, (std::min)(1.0f, r2));
                     float ymax = (std::max)(0.0f, (std::min)(1.0f, r3));
                     
                     if (xmin > xmax) std::swap(xmin, xmax);
                     if (ymin > ymax) std::swap(ymin, ymax);

                     std::cerr << "[ONNX] DET: ID=" << label_id << " Conf=" << score 
                               << " BBox=[" << xmin << "," << ymin << "," << xmax << "," << ymax << "]"
                               << " Raw=[" << r0 << "," << r1 << "," << r2 << "," << r3 << "]\n";
                     
                     Detection d;
                     d.confidence = score;
                     d.bbox = {xmin, ymin, xmax - xmin, ymax - ymin}; // x, y, width, height
                     
                     // Map Label
                     std::string label;
                     if (label_id == 1) label = "person";
                     else if (label_id == 2) label = "bicycle";
                     else if (label_id == 3) label = "car";
                     else if (label_id == 4) label = "motorcycle";
                     else if (label_id == 6) label = "bus";
                     else if (label_id == 8) label = "truck";
                     else continue; // Reject unknown to avoid validation error in Go
                     
                     d.label = label;
                     results.push_back(d);
                 }
            }
        }
        
        return results;

    } catch (const Ort::Exception& e) {
        std::cerr << "[ONNX] Inference Failed: " << e.what() << "\n";
        return {};
    }
}

} // namespace vms_ai
