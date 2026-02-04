#pragma once
#include <string>
#include <atomic>

namespace ts::vms::media::pipeline {

enum class State {
    STOPPED,
    STARTING,
    RUNNING,
    STALLED,
    RECONNECTING
};

class PipelineFSM {
public:
    PipelineFSM();
    
    void TransitionTo(State next_state);
    State GetCurrentState() const;
    static std::string StateToString(State state);

private:
    std::atomic<State> current_state_;
};

} // namespace ts::vms::media::pipeline
