#include "DiskMetrics.h"
#include <iostream>
#include <pdh.h>
#include <pdhmsg.h>

namespace ts { namespace vms { namespace diskio {

DiskMetrics::DiskMetrics() {
#ifdef _WIN32
    if (PdhOpenQuery(NULL, 0, (PDH_HQUERY*)&pdh_query_) != ERROR_SUCCESS) {
        std::cerr << "[DiskMetrics] PdhOpenQuery failed.\n";
        return;
    }

    std::string path = "\\LogicalDisk(C:)\\Current Disk Queue Length";
    if (PdhAddCounterA((PDH_HQUERY)pdh_query_, path.c_str(), 0, (PDH_HCOUNTER*)&pdh_counter_) != ERROR_SUCCESS) {
        std::cerr << "[DiskMetrics] PdhAddCounter failed for " << path << ".\n";
    }
#endif
}

DiskMetrics::~DiskMetrics() {
#ifdef _WIN32
    if (pdh_query_) {
        PdhCloseQuery((PDH_HQUERY)pdh_query_);
    }
#endif
}

void DiskMetrics::Update() {
#ifdef _WIN32
    if (!pdh_query_ || !pdh_counter_) return;

    PdhCollectQueryData((PDH_HQUERY)pdh_query_);
    PDH_FMT_COUNTERVALUE counterVal;
    
    if (PdhGetFormattedCounterValue((PDH_HCOUNTER)pdh_counter_, PDH_FMT_DOUBLE, NULL, &counterVal) == ERROR_SUCCESS) {
        current_queue_depth_.store(counterVal.doubleValue);
    }
#endif
}

}}}
