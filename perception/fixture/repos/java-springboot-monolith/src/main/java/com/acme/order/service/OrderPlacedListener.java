package com.acme.order.service;

import com.acme.order.event.OrderPlaced;

/** Plain interface, injected by setter in NotificationService. */
public interface OrderPlacedListener {

    void onPlaced(OrderPlaced event);
}
