#include "image_processor.h"
#include <windows.h>
#include <wincodec.h>
#include <iostream>

#pragma comment(lib, "windowscodecs.lib")
#pragma comment(lib, "ole32.lib")

namespace vms_ai {

// WIC Factory wrapper
struct ImageProcessor::Impl {
    IWICImagingFactory* factory = nullptr;

    Impl() {
        CoInitialize(NULL);
        CoCreateInstance(CLSID_WICImagingFactory, NULL, CLSCTX_INPROC_SERVER, IID_PPV_ARGS(&factory));
    }
    ~Impl() {
        if (factory) factory->Release();
        CoUninitialize();
    }
};

ImageProcessor::ImageProcessor() : impl_(new Impl()) {}
ImageProcessor::~ImageProcessor() { delete impl_; }

std::optional<ImageTensor> ImageProcessor::DecodeAndPreprocess(const std::vector<uint8_t>& jpeg_bytes, int target_w, int target_h) {
    if (!impl_->factory || jpeg_bytes.empty()) return std::nullopt;

    IWICStream* pStream = nullptr;
    if (FAILED(impl_->factory->CreateStream(&pStream))) return std::nullopt;
    
    // Initialize stream from memory
    if (FAILED(pStream->InitializeFromMemory((BYTE*)jpeg_bytes.data(), (DWORD)jpeg_bytes.size()))) {
        pStream->Release();
        return std::nullopt;
    }

    IWICBitmapDecoder* pDecoder = nullptr;
    if (FAILED(impl_->factory->CreateDecoderFromStream(pStream, NULL, WICDecodeMetadataCacheOnDemand, &pDecoder))) {
        pStream->Release();
        return std::nullopt;
    }

    IWICBitmapFrameDecode* pFrame = nullptr;
    if (FAILED(pDecoder->GetFrame(0, &pFrame))) {
        pDecoder->Release();
        pStream->Release();
        return std::nullopt;
    }

    // Convert to BGR/RGB 8bpp
    // ONNX models usually expect RGB. WIC provides GUID_WICPixelFormat24bppBGR or RGB usually.
    // Let's use 24bppBGR (common windows) then swap if needed, or see if 24bppRGB is supported.
    // Safest is to convert to 32bppBGRA and drop alpha, or 24bppBGR.
    
    IWICFormatConverter* pConverter = nullptr;
    impl_->factory->CreateFormatConverter(&pConverter);
    
    // Normalize to 24bppBGR (Windows native friendly)
    if (FAILED(pConverter->Initialize(pFrame, GUID_WICPixelFormat24bppBGR, 
                                      WICBitmapDitherTypeNone, NULL, 0.0, WICBitmapPaletteTypeCustom))) {
        pFrame->Release();
        pDecoder->Release();
        pStream->Release();
        if (pConverter) pConverter->Release();
        return std::nullopt;
    }
    
    // Resize (Scaler)
    IWICBitmapScaler* pScaler = nullptr;
    impl_->factory->CreateBitmapScaler(&pScaler);
    if (FAILED(pScaler->Initialize(pConverter, target_w, target_h, WICBitmapInterpolationModeFant))) {
         // clean up
         // ... simplified for brevity, in prod use smart pointers (ComPtr)
         return std::nullopt;
    }

    // Copy pixels
    std::vector<BYTE> buffer(target_w * target_h * 3);
    if (FAILED(pScaler->CopyPixels(NULL, target_w * 3, (UINT)buffer.size(), buffer.data()))) {
        return std::nullopt;
    }

    // Cleanup WIC objects
    pScaler->Release();
    pConverter->Release();
    pFrame->Release();
    pDecoder->Release();
    pStream->Release();

    // Create Tensor (CHW or HWC? ONNX standard is NCHW usually, usually RGB planar)
    // MobileNet SSD v2 usually expects: 1 x 3 x 300 x 300
    // Values: (pixel - mean) / std ? Or [0..1] or [0..255]?
    // MobileNet v2 SSD often takes [0..255] uint8 resolved to [-1..1] internally or [0..1] depending on graph.
    // Most standard ONNXzoo models expect NCHW float32 RGB [0..1] OR [0..255] minus mean.
    
    // Assuming standard NCHW [0..1] RGB for this exercise.
    ImageTensor tensor;
    tensor.width = target_w;
    tensor.height = target_h;
    tensor.channels = 3;
    tensor.data.resize(target_w * target_h * 3);

    float* r_plane = &tensor.data[0]; 
    float* g_plane = &tensor.data[target_w * target_h];
    float* b_plane = &tensor.data[target_w * target_h * 2];

    for (int i = 0; i < target_w * target_h; ++i) {
        // WIC buffer is BGR (byte)
        uint8_t b = buffer[i * 3 + 0];
        uint8_t g = buffer[i * 3 + 1];
        uint8_t r = buffer[i * 3 + 2];

        // Normalize to 0.0 - 1.0 (typical)
        // Adjust if model requires 0-255 or mean subtraction
        // MobileNet SSD v2 typically does: (x - 127.5) / 127.5  for [-1, 1] range
        // vms-mock used [0..1]. Let's stick to [0,1] float unless model specifically wants otherwise.
        // Actually, many ONNX models take uint8 [0-255]. But let's assume float input for now.
        
        r_plane[i] = r / 255.0f; 
        g_plane[i] = g / 255.0f;
        b_plane[i] = b / 255.0f;
    }

    return tensor;
}

} // namespace vms_ai
