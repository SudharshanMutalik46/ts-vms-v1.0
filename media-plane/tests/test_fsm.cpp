#include <gtest/gtest.h>
#include "pipeline/pipeline_fsm.hpp"

using namespace ts::vms::media::pipeline;

TEST(PipelineFSMTest, InitialStateIsStopped) {
    PipelineFSM fsm;
    EXPECT_EQ(fsm.GetCurrentState(), State::STOPPED);
}

TEST(PipelineFSMTest, TransitionWorks) {
    PipelineFSM fsm;
    fsm.TransitionTo(State::STARTING);
    EXPECT_EQ(fsm.GetCurrentState(), State::STARTING);
    fsm.TransitionTo(State::RUNNING);
    EXPECT_EQ(fsm.GetCurrentState(), State::RUNNING);
}

TEST(PipelineFSMTest, StateToString) {
    PipelineFSM fsm;
    EXPECT_EQ(fsm.StateToString(State::STOPPED), "STOPPED");
    EXPECT_EQ(fsm.StateToString(State::RUNNING), "RUNNING");
    EXPECT_EQ(fsm.StateToString(State::STALLED), "STALLED");
}
