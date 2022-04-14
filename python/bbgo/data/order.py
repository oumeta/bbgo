from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime

import bbgo_pb2

from ..enums import OrderType
from ..enums import SideType


@dataclass
class Order:
    exchange: str
    symbol: str
    order_id: str
    side: SideType
    order_type: OrderType
    price: float
    stop_price: float
    status: str
    quantity: float
    executed_quantity: float
    client_order_id: str
    group_id: int
    created_at: datetime

    @classmethod
    def from_pb(cls, obj: bbgo_pb2.Order) -> Order:
        return cls(
            exchange=obj.exchange,
            symbol=obj.symbol,
            order_id=obj.id,
            side=SideType.from_pb(obj.side),
            order_type=OrderType.from_pb(obj.order_type),
            price=float(obj.price),
            stop_price=float(obj.stop_price),
            status=obj.status,
            quantity=float(obj.quantity),
            executed_quantity=float(obj.executed_quantity),
            client_order_id=obj.client_order_id,
            group_id=obj.group_id,
            created_at=datetime.fromtimestamp(obj.created_at / 1000),
        )
