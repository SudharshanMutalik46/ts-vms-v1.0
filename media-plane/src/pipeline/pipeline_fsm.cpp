#include "pipeline_fsm.hpp"

namespace ts::vms::media::pipeline {

PipelineFSM::PipelineFSM() : current_state_(State::STOPPED) {}

void PipelineFSM::TransitionTo(State next_state) {
    current_state_.store(next_state);
}

State PipelineFSM::GetCurrentState() const {
    return current_state_.load();
}

std::string PipelineFSM::StateToString(State state) {
    switch (state) {
        case State::STOPPED: return "STOPPED";
        case State::STARTING: return "STARTING";
        case State::RUNNING: return "RUNNING";
        case State::STALLED: return "STALLED";
        case State::RECONNECTING: return "RECONNECTING";
        default: return "UNKNOWN";
    }
}

} // namespace ts::vms::media::pipeline
