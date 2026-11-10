import React from 'react';
import { displayStatus } from '../utils/orders.js';
import '../css/StatusBadge.css';

export default function StatusBadge({ order }) {
  const label = displayStatus(order);
  return <span className={`status st-${label.toLowerCase()}`}><i />{label}</span>;
}