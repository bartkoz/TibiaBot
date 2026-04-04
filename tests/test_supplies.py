from cavebot.survival.supplies import SupplyTracker


def test_supply_tracker_initial():
    st = SupplyTracker(max_potions=200, refill_threshold=20)
    assert st.needs_refill() is False
    assert st.remaining == 200


def test_supply_tracker_use():
    st = SupplyTracker(max_potions=200, refill_threshold=20)
    st.use_potion()
    assert st.remaining == 199


def test_supply_tracker_needs_refill():
    st = SupplyTracker(max_potions=200, refill_threshold=20)
    for _ in range(185):
        st.use_potion()
    assert st.remaining == 15
    assert st.needs_refill() is True


def test_supply_tracker_refill():
    st = SupplyTracker(max_potions=200, refill_threshold=20)
    for _ in range(190):
        st.use_potion()
    st.refill()
    assert st.remaining == 200
    assert st.needs_refill() is False
