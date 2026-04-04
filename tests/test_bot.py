from cavebot.core.bot import BotState, BotStateMachine


def test_initial_state():
    sm = BotStateMachine()
    assert sm.state == BotState.IDLE


def test_start_transitions_to_walking():
    sm = BotStateMachine()
    sm.start()
    assert sm.state == BotState.WALKING


def test_stop_transitions_to_idle():
    sm = BotStateMachine()
    sm.start()
    sm.stop()
    assert sm.state == BotState.IDLE


def test_transition_to_combat():
    sm = BotStateMachine()
    sm.start()
    sm.transition(BotState.COMBAT)
    assert sm.state == BotState.COMBAT


def test_transition_combat_to_looting():
    sm = BotStateMachine()
    sm.start()
    sm.transition(BotState.COMBAT)
    sm.transition(BotState.LOOTING)
    assert sm.state == BotState.LOOTING


def test_transition_looting_to_walking():
    sm = BotStateMachine()
    sm.start()
    sm.transition(BotState.COMBAT)
    sm.transition(BotState.LOOTING)
    sm.transition(BotState.WALKING)
    assert sm.state == BotState.WALKING


def test_status_dict():
    sm = BotStateMachine()
    sm.start()
    status = sm.status()
    assert status["state"] == "WALKING"
    assert "position" in status
    assert "health_pct" in status
    assert "mana_pct" in status
