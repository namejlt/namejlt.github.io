---
title: "任天堂NES象棋游戏AI设计"
date: 2025-05-29T15:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["AI", "游戏"]
---

## 问题


早期任天堂nes棋类游戏有电脑选项，它们的硬件限制相当苛刻，程序员是如何实现简单难度、中等难度、高等难度的，采用什么算法，具体如何实现？


## 回答

在任天堂NES的棋类游戏中，电脑AI的设计需在硬件限制下实现高效决策。以象棋游戏为例，其核心原理涉及规则编码、搜索算法优化和评估函数设计，以下从技术实现角度展开分析。

### 一、硬件限制与算法选型
NES的处理器主频仅1.7 MHz，内存约64KB，无法支持现代深度神经网络或复杂树搜索。因此，开发者采用经典博弈论算法，如**Minimax（极大极小算法）**结合**Alpha-Beta剪枝**，并通过启发式规则降低计算量。例如，限制搜索深度至3-4层，同时利用位操作优化数据处理速度。

### 二、规则编码与棋盘表示
1. **棋子状态数字化**  
   使用二维数组（如8x8或9x10）表示棋盘，每个元素存储棋子类型（如车、马、炮）和颜色。例如，用1表示红方车，-1表示黑方车，0表示空位。这种结构便于快速访问和修改棋盘状态。

2. **移动规则硬编码**  
   针对每种棋子编写独立的移动生成函数。例如，马的“日”字移动需检查是否有“别腿”，炮的移动需计算路径上的棋子数量。这些规则通过条件判断实现，确保生成的移动合法。

### 三、搜索算法与剪枝优化
1. **Minimax算法**  
   递归遍历棋局树，假设双方均采取最优策略：  
   - 己方（MAX层）选择得分最高的移动；  
   - 对方（MIN层）选择得分最低的移动。  
   由于计算量随深度呈指数级增长（如每步50种可能，4层即50⁴=6,250,000种状态），需结合剪枝技术。

2. **Alpha-Beta剪枝**  
   在搜索过程中动态调整α（当前路径的最大得分）和β（当前路径的最小得分），若子节点得分超出α/β范围则终止搜索。例如，若某路径已找到比当前最优解更差的结果，后续分支无需继续计算。这一优化可将时间复杂度从O(b^d)降至O(b^(d/2))，显著提升效率。

3. **历史启发式优化**  
   记录曾带来高分的移动，优先搜索这些“历史得分”高的路径。例如，若某步棋多次导致胜利，则在后续搜索中提前评估该移动，更早触发剪枝。此方法可将计算速度提升6-7倍。

### 四、评估函数设计
评估函数是AI决策的核心，需量化局面优劣。典型设计包括：
1. **子力价值**  
   为每种棋子赋予固定分值（如车950，马450，炮450），总分差反映实力对比。

2. **位置修正**  
   棋子位置影响威胁性。例如，兵过河后威胁增加，将帅靠近九宫中心更安全。通过预定义位置权重表调整得分。

3. **灵活度与威胁值**  
   计算棋子可移动步数（灵活度）和被攻击次数（威胁值）。例如，车在空旷区域灵活度高，被炮瞄准则威胁值上升。

4. **战术组合**  
   识别特定战术（如当头炮、铁门栓），通过模式匹配触发额外加分或扣分。例如，形成“双车错”杀局时大幅提升评估值。

### 五、难度分级与策略调整
1. **搜索深度控制**  
   简单难度使用2-3层搜索，复杂难度增加至4-5层。更深的搜索能预判更多步，但计算时间更长。

2. **评估函数权重调节**  
   调整子力价值、位置修正等参数。例如，困难模式侧重战术组合得分，简单模式优先保护高价值棋子。

3. **开局库与残局策略**  
   预存常见开局走法（如中炮、屏风马）和残局必胜定式（如单车必胜士象全），直接调用而非实时计算，减少计算量。

### 六、硬件优化与实现技巧
1. **位操作加速**  
   用位掩码表示棋子位置（如每个bit代表一个棋盘格），通过位运算快速判断移动合法性。例如，车的直线移动可用按位与运算检测路径是否畅通。

2. **内存管理**  
   复用内存空间存储当前棋局、历史得分表和搜索路径，避免频繁分配/释放内存。例如，使用循环队列缓存最近的移动历史。

3. **异步计算**  
   利用NES的DMA（直接内存访问）技术，在渲染画面的同时后台计算下一步，减少玩家等待时间。

### 七、案例分析：NES象棋游戏的典型实现
以某款NES象棋游戏为例，其AI系统可能包含以下模块：
- **移动生成器**：遍历所有棋子，生成合法移动列表。  
- **评估器**：计算当前局面得分，考虑子力、位置、威胁等因素。  
- **搜索器**：使用Alpha-Beta剪枝递归搜索，深度4层，结合历史启发式排序。  
- **界面控制器**：处理玩家输入，更新棋盘显示，并调用AI模块生成响应。

在实际运行中，该AI可能每秒评估约10,000个局面（搜索深度4层时），通过优化可在NES硬件上流畅运行，提供接近人类中级水平的对弈体验。

### 八、总结
NES象棋游戏的AI设计是算法优化与硬件限制博弈的产物。通过Minimax算法、Alpha-Beta剪枝、启发式评估函数和位操作优化，开发者在有限资源下实现了高效决策。尽管受限于硬件性能，其设计思想（如规则硬编码、搜索剪枝）为后续棋类AI发展奠定了基础，也为现代游戏在资源约束下的智能系统设计提供了借鉴。

## 代码实现参考


重点：深度遍历（深度越深，认为智能越高）、剪枝（减少计算消耗）、分数计算（评估行为策略有效性）


### 核心逻辑

#### 核心功能：

- 棋盘表示：使用二维数组存储棋子位置，每个元素代表一个棋盘格
- 移动规则：为每种棋子实现了基本移动规则（如马走日、象走田）
- AI 决策：使用 Minimax 算法和 Alpha-Beta 剪枝计算最优移动
- 难度调整：通过设置搜索深度控制 AI 难度（1-5 级）
- 局面评估：基于棋子价值和简单位置修正评估当前局面

#### 设计思路：

- 搜索深度限制在 3-5 层以平衡计算量
- 使用 Alpha-Beta 剪枝减少不必要的计算
- 通过位置修正（如过河卒子加分）提升 AI 表现
- 开局阶段简化计算以保持响应速度

### 代码实现

```python
import time
import random
from typing import List, Tuple, Dict, Optional

# 棋子类型常量
EMPTY = 0
RED = 1
BLACK = -1
RED_KING = 1
RED_ADVISOR = 2
RED_ELEPHANT = 3
RED_HORSE = 4
RED_CHARIOT = 5
RED_CANNON = 6
RED_PAWN = 7
BLACK_KING = -1
BLACK_ADVISOR = -2
BLACK_ELEPHANT = -3
BLACK_HORSE = -4
BLACK_CHARIOT = -5
BLACK_CANNON = -6
BLACK_PAWN = -7

# 棋子价值（简化版）
PIECE_VALUES = {
    RED_KING: 10000,
    RED_ADVISOR: 200,
    RED_ELEPHANT: 200,
    RED_HORSE: 400,
    RED_CHARIOT: 900,
    RED_CANNON: 450,
    RED_PAWN: 100,
    BLACK_KING: -10000,
    BLACK_ADVISOR: -200,
    BLACK_ELEPHANT: -200,
    BLACK_HORSE: -400,
    BLACK_CHARIOT: -900,
    BLACK_CANNON: -450,
    BLACK_PAWN: -100,
    EMPTY: 0
}

# 棋盘大小
BOARD_ROWS = 10
BOARD_COLS = 9

RED_COLOR = "\033[1;31m"      # 红色加粗
BLACK_COLOR = "\033[1;34m"    # 蓝色加粗
BG_COLOR = "\033[47m"         # 白底（可换成\033[100m灰底或\033[43m黄底）
RESET_COLOR = "\033[0m"       # 恢复默认

class ChineseChess:
    def __init__(self):
        # 初始化棋盘
        self.board = [[EMPTY for _ in range(BOARD_COLS)] for _ in range(BOARD_ROWS)]
        self.initialize_board()
        self.current_player = RED  # 红方先行
        self.move_history = []
        self.search_depth = 3  # 默认搜索深度
        
    def initialize_board(self):
        # 初始化棋盘布局（简化版）
        # 黑方
        self.board[0][0] = BLACK_CHARIOT
        self.board[0][1] = BLACK_HORSE
        self.board[0][2] = BLACK_ELEPHANT
        self.board[0][3] = BLACK_ADVISOR
        self.board[0][4] = BLACK_KING
        self.board[0][5] = BLACK_ADVISOR
        self.board[0][6] = BLACK_ELEPHANT
        self.board[0][7] = BLACK_HORSE
        self.board[0][8] = BLACK_CHARIOT
        self.board[2][1] = BLACK_CANNON
        self.board[2][7] = BLACK_CANNON
        self.board[3][0] = BLACK_PAWN
        self.board[3][2] = BLACK_PAWN
        self.board[3][4] = BLACK_PAWN
        self.board[3][6] = BLACK_PAWN
        self.board[3][8] = BLACK_PAWN
        
        # 红方
        self.board[9][0] = RED_CHARIOT
        self.board[9][1] = RED_HORSE
        self.board[9][2] = RED_ELEPHANT
        self.board[9][3] = RED_ADVISOR
        self.board[9][4] = RED_KING
        self.board[9][5] = RED_ADVISOR
        self.board[9][6] = RED_ELEPHANT
        self.board[9][7] = RED_HORSE
        self.board[9][8] = RED_CHARIOT
        self.board[7][1] = RED_CANNON
        self.board[7][7] = RED_CANNON
        self.board[6][0] = RED_PAWN
        self.board[6][2] = RED_PAWN
        self.board[6][4] = RED_PAWN
        self.board[6][6] = RED_PAWN
        self.board[6][8] = RED_PAWN
    
    def is_in_palace(self, row, col, piece_type):
        # 判断是否在九宫格内
        if piece_type > 0:  # 红方
            return 7 <= row <= 9 and 3 <= col <= 5
        else:  # 黑方
            return 0 <= row <= 2 and 3 <= col <= 5
    
    def get_valid_moves(self, row, col):
        # 获取指定棋子的合法移动（简化版）
        piece = self.board[row][col]
        if piece == EMPTY:
            return []
            
        moves = []
        is_red = piece > 0
        
        # 根据棋子类型生成移动
        if abs(piece) == RED_KING:
            # 将/帅移动规则（九宫格内）
            directions = [(-1, 0), (1, 0), (0, -1), (0, 1)]
            for dr, dc in directions:
                new_row, new_col = row + dr, col + dc
                if 0 <= new_row < BOARD_ROWS and 0 <= new_col < BOARD_COLS:
                    if self.is_in_palace(new_row, new_col, piece):
                        target = self.board[new_row][new_col]
                        if target == EMPTY or (target > 0) != is_red:
                            moves.append((new_row, new_col))
        
        elif abs(piece) == RED_ADVISOR:
            # 士/仕移动规则（九宫格斜线）
            directions = [(-1, -1), (-1, 1), (1, -1), (1, 1)]
            for dr, dc in directions:
                new_row, new_col = row + dr, col + dc
                if 0 <= new_row < BOARD_ROWS and 0 <= new_col < BOARD_COLS:
                    if self.is_in_palace(new_row, new_col, piece):
                        target = self.board[new_row][new_col]
                        if target == EMPTY or (target > 0) != is_red:
                            moves.append((new_row, new_col))
        
        elif abs(piece) == RED_ELEPHANT:
            # 象/相移动规则（田字格）
            directions = [(-2, -2), (-2, 2), (2, -2), (2, 2)]
            for dr, dc in directions:
                new_row, new_col = row + dr, col + dc
                if 0 <= new_row < BOARD_ROWS and 0 <= new_col < BOARD_COLS:
                    # 象不能过河
                    if (is_red and new_row >= 5) or (not is_red and new_row <= 4):
                        # 检查绊象腿
                        eye_row, eye_col = row + dr//2, col + dc//2
                        if self.board[eye_row][eye_col] == EMPTY:
                            target = self.board[new_row][new_col]
                            if target == EMPTY or (target > 0) != is_red:
                                moves.append((new_row, new_col))
        
        elif abs(piece) == RED_HORSE:
            # 马移动规则（日字格）
            directions = [
                (-2, -1, -1, 0), (-2, 1, -1, 0),
                (2, -1, 1, 0), (2, 1, 1, 0),
                (-1, -2, 0, -1), (-1, 2, 0, 1),
                (1, -2, 0, -1), (1, 2, 0, 1)
            ]
            for dr, dc, check_r, check_c in directions:
                new_row, new_col = row + dr, col + dc
                if 0 <= new_row < BOARD_ROWS and 0 <= new_col < BOARD_COLS:
                    # 检查绊马腿
                    check_row, check_col = row + check_r, col + check_c
                    if self.board[check_row][check_col] == EMPTY:
                        target = self.board[new_row][new_col]
                        if target == EMPTY or (target > 0) != is_red:
                            moves.append((new_row, new_col))
        
        elif abs(piece) == RED_CHARIOT:
            # 车移动规则（直线移动）
            directions = [(-1, 0), (1, 0), (0, -1), (0, 1)]
            for dr, dc in directions:
                new_row, new_col = row + dr, col + dc
                while 0 <= new_row < BOARD_ROWS and 0 <= new_col < BOARD_COLS:
                    target = self.board[new_row][new_col]
                    if target == EMPTY:
                        moves.append((new_row, new_col))
                    elif (target > 0) != is_red:
                        moves.append((new_row, new_col))
                        break
                    else:
                        break
                    new_row += dr
                    new_col += dc
        
        elif abs(piece) == RED_CANNON:
            # 炮移动规则（直线移动，翻山吃子）
            directions = [(-1, 0), (1, 0), (0, -1), (0, 1)]
            for dr, dc in directions:
                jumped = False
                new_row, new_col = row + dr, col + dc
                while 0 <= new_row < BOARD_ROWS and 0 <= new_col < BOARD_COLS:
                    target = self.board[new_row][new_col]
                    if target == EMPTY:
                        if not jumped:
                            moves.append((new_row, new_col))
                    else:
                        if not jumped:
                            jumped = True
                        else:
                            if (target > 0) != is_red:
                                moves.append((new_row, new_col))
                            break
                    new_row += dr
                    new_col += dc
        
        elif abs(piece) == RED_PAWN:
            # 卒/兵移动规则
            if is_red:
                directions = [(-1, 0)]  # 向前
                if row <= 4:  # 过河
                    directions.extend([(0, -1), (0, 1)])  # 可以左右移动
            else:
                directions = [(1, 0)]  # 向前
                if row >= 5:  # 过河
                    directions.extend([(0, -1), (0, 1)])  # 可以左右移动
            
            for dr, dc in directions:
                new_row, new_col = row + dr, col + dc
                if 0 <= new_row < BOARD_ROWS and 0 <= new_col < BOARD_COLS:
                    target = self.board[new_row][new_col]
                    if target == EMPTY or (target > 0) != is_red:
                        moves.append((new_row, new_col))
        
        return moves
    
    def evaluate_position(self):
        # 评估当前局面得分（简化版）
        score = 0
        for row in range(BOARD_ROWS):
            for col in range(BOARD_COLS):
                piece = self.board[row][col]
                score += PIECE_VALUES[piece]
                
                # 简单位置修正（例如过河的卒子加分）
                if piece == RED_PAWN and row <= 4:
                    score += 50  # 过河卒子威力大
                elif piece == BLACK_PAWN and row >= 5:
                    score -= 50
        
        return score
    
    def make_move(self, from_row, from_col, to_row, to_col):
        # 执行一步移动
        piece = self.board[from_row][from_col]
        captured = self.board[to_row][to_col]
        self.board[from_row][from_col] = EMPTY
        self.board[to_row][to_col] = piece
        self.move_history.append((from_row, from_col, to_row, to_col, captured))
        self.current_player = -self.current_player  # 切换玩家
        return captured
    
    def undo_move(self):
        # 撤销最后一步移动
        if not self.move_history:
            return
            
        from_row, from_col, to_row, to_col, captured = self.move_history.pop()
        piece = self.board[to_row][to_col]
        self.board[from_row][from_col] = piece
        self.board[to_row][to_col] = captured
        self.current_player = -self.current_player  # 切换回上一个玩家
    
    def is_game_over(self):
        # 检查游戏是否结束（一方将/帅被吃掉）
        red_king_exists = False
        black_king_exists = False
        
        for row in range(BOARD_ROWS):
            for col in range(BOARD_COLS):
                if self.board[row][col] == RED_KING:
                    red_king_exists = True
                elif self.board[row][col] == BLACK_KING:
                    black_king_exists = True
        
        return not red_king_exists or not black_king_exists
    
    def get_all_valid_moves(self, player):
        # 获取当前玩家的所有合法移动
        moves = []
        for row in range(BOARD_ROWS):
            for col in range(BOARD_COLS):
                if self.board[row][col] * player > 0:  # 是当前玩家的棋子
                    valid_moves = self.get_valid_moves(row, col)
                    for to_row, to_col in valid_moves:
                        moves.append((row, col, to_row, to_col))
        return moves
    
    def minimax(self, depth, maximizing_player, alpha, beta):
        # Minimax算法实现，带Alpha-Beta剪枝
        if depth == 0 or self.is_game_over():
            return self.evaluate_position(), None
        
        if maximizing_player:
            max_score = float('-inf')
            best_move = None
            moves = self.get_all_valid_moves(RED)
            random.shuffle(moves)  # 随机打乱顺序，增加变化性
            
            for move in moves:
                from_row, from_col, to_row, to_col = move
                captured = self.make_move(from_row, from_col, to_row, to_col)
                score, _ = self.minimax(depth - 1, False, alpha, beta)
                self.undo_move()
                
                if score > max_score:
                    max_score = score
                    best_move = move
                
                alpha = max(alpha, score)
                if beta <= alpha:
                    break  # Beta剪枝
            
            return max_score, best_move
        else:
            min_score = float('inf')
            best_move = None
            moves = self.get_all_valid_moves(BLACK)
            random.shuffle(moves)  # 随机打乱顺序，增加变化性
            
            for move in moves:
                from_row, from_col, to_row, to_col = move
                captured = self.make_move(from_row, from_col, to_row, to_col)
                score, _ = self.minimax(depth - 1, True, alpha, beta)
                self.undo_move()
                
                if score < min_score:
                    min_score = score
                    best_move = move
                
                beta = min(beta, score)
                if beta <= alpha:
                    break  # Alpha剪枝
            
            return min_score, best_move
    
    def get_computer_move(self):
        # 获取电脑的最佳移动
        start_time = time.time()
        
        # 根据难度调整搜索深度
        if len(self.move_history) < 10:  # 开局阶段
            depth = min(self.search_depth - 1, 2)  # 开局减少计算量
        else:
            depth = self.search_depth
        
        if self.current_player == RED:
            _, best_move = self.minimax(depth, True, float('-inf'), float('inf'))
        else:
            _, best_move = self.minimax(depth, False, float('-inf'), float('inf'))
        
        end_time = time.time()
        print(f"AI思考时间: {end_time - start_time:.2f}秒")
        
        return best_move
    
    def set_difficulty(self, difficulty):
        # 设置AI难度（1-5）
        self.search_depth = min(max(difficulty, 1), 5)
        print(f"AI难度已设置为: {difficulty}")
    
    def print_board(self):
        # 打印棋盘（对齐优化版）
        col_labels = "  " + "  ".join(str(i) for i in range(BOARD_COLS))
        print(col_labels)
        print(" ---------------------------")
        for i, row in enumerate(self.board):
            line = f"{i}|"
            for cell in row:
                if cell == EMPTY:
                    line += f"{BG_COLOR}·  {RESET_COLOR}"
                elif cell == RED_KING:
                    line += f"{BG_COLOR}{RED_COLOR}帅 {RESET_COLOR}"
                elif cell == RED_ADVISOR:
                    line += f"{BG_COLOR}{RED_COLOR}仕 {RESET_COLOR}"
                elif cell == RED_ELEPHANT:
                    line += f"{BG_COLOR}{RED_COLOR}相 {RESET_COLOR}"
                elif cell == RED_HORSE:
                    line += f"{BG_COLOR}{RED_COLOR}马 {RESET_COLOR}"
                elif cell == RED_CHARIOT:
                    line += f"{BG_COLOR}{RED_COLOR}车 {RESET_COLOR}"
                elif cell == RED_CANNON:
                    line += f"{BG_COLOR}{RED_COLOR}炮 {RESET_COLOR}"
                elif cell == RED_PAWN:
                    line += f"{BG_COLOR}{RED_COLOR}兵 {RESET_COLOR}"
                elif cell == BLACK_KING:
                    line += f"{BG_COLOR}{BLACK_COLOR}將 {RESET_COLOR}"
                elif cell == BLACK_ADVISOR:
                    line += f"{BG_COLOR}{BLACK_COLOR}士 {RESET_COLOR}"
                elif cell == BLACK_ELEPHANT:
                    line += f"{BG_COLOR}{BLACK_COLOR}象 {RESET_COLOR}"
                elif cell == BLACK_HORSE:
                    line += f"{BG_COLOR}{BLACK_COLOR}馬 {RESET_COLOR}"
                elif cell == BLACK_CHARIOT:
                    line += f"{BG_COLOR}{BLACK_COLOR}車 {RESET_COLOR}"
                elif cell == BLACK_CANNON:
                    line += f"{BG_COLOR}{BLACK_COLOR}砲 {RESET_COLOR}"
                elif cell == BLACK_PAWN:
                    line += f"{BG_COLOR}{BLACK_COLOR}卒 {RESET_COLOR}"
            print(line)

# 游戏主循环
def play_game():
    game = ChineseChess()
    game.set_difficulty(3)  # 中等难度
    
    while not game.is_game_over():
        game.print_board()
        
        if game.current_player == RED:
            print("轮到红方（玩家）")
            while True:
                try:
                    from_row = int(input("请输入起始行 (0-9): "))
                    from_col = int(input("请输入起始列 (0-8): "))
                    to_row = int(input("请输入目标行 (0-9): "))
                    to_col = int(input("请输入目标列 (0-8): "))
                    
                    if game.board[from_row][from_col] <= 0:
                        print("请选择红方棋子")
                        continue
                    
                    valid_moves = game.get_valid_moves(from_row, from_col)
                    if (to_row, to_col) not in valid_moves:
                        print("无效的移动")
                        continue
                    
                    game.make_move(from_row, from_col, to_row, to_col)
                    break
                except (ValueError, IndexError):
                    print("输入无效，请重新输入")
        else:
            print("轮到黑方（电脑）")
            best_move = game.get_computer_move()
            if best_move:
                from_row, from_col, to_row, to_col = best_move
                print(f"电脑移动: {from_row},{from_col} -> {to_row},{to_col}")
                game.make_move(from_row, from_col, to_row, to_col)
            else:
                print("电脑没有合法移动，游戏结束")
                break
    
    game.print_board()
    if game.current_player == RED:
        print("黑方获胜！")
    else:
        print("红方获胜！")

if __name__ == "__main__":
    play_game()    

```


### 代码优化

#### 分数评估优化

0. 问题重述  
当前 `evaluate_position` 只考虑棋子基础价值和过河兵加分，评估过于粗糙，不能准确反映真实局势。

1. 关键点分析  
- 仅用棋子价值+过河兵，忽略了棋子位置、活跃度、保护、威胁、将帅安全等重要因素。
- 评估函数越丰富，AI决策越“像人”。

2. 优化方向  
- 位置分：不同棋子在不同位置有不同价值（如马在中路更灵活，车在底线更安全）。
- 保护分：己方棋子被己方保护加分。
- 威胁分：己方棋子威胁对方棋子加分。
- 将帅安全分：将/帅暴露或被威胁扣分。
- 兵线分：兵/卒推进越深加分。
- 活跃度分：可走步数越多加分。

3. 推荐优化实现（简化版，易于理解和扩展）

```python
def evaluate_position(self):
    score = 0
    # 位置分模板（可细化）
    PAWN_POSITION_BONUS = [
        [0, 0, 0, 0, 0, 0, 0, 0, 0],
        [5, 5, 5, 5, 10, 5, 5, 5, 5],
        [4, 4, 8, 8, 10, 8, 8, 4, 4],
        [3, 3, 6, 6, 10, 6, 6, 3, 3],
        [2, 2, 4, 4, 10, 4, 4, 2, 2],
        [0, 0, 0, 0, 0, 0, 0, 0, 0],
        [0, 0, 0, 0, 0, 0, 0, 0, 0],
        [0, 0, 0, 0, 0, 0, 0, 0, 0],
        [0, 0, 0, 0, 0, 0, 0, 0, 0],
        [0, 0, 0, 0, 0, 0, 0, 0, 0],
    ]
    for row in range(BOARD_ROWS):
        for col in range(BOARD_COLS):
            piece = self.board[row][col]
            score += PIECE_VALUES[piece]
            # 过河兵加分+位置分
            if piece == RED_PAWN:
                if row <= 4:
                    score += 50
                score += PAWN_POSITION_BONUS[row][col]
            elif piece == BLACK_PAWN:
                if row >= 5:
                    score -= 50
                score -= PAWN_POSITION_BONUS[9-row][col]  # 黑方兵位置分镜像
            # 简单保护分
            if piece != EMPTY:
                for dr, dc in [(-1,0),(1,0),(0,-1),(0,1)]:
                    nr, nc = row+dr, col+dc
                    if 0<=nr<BOARD_ROWS and 0<=nc<BOARD_COLS:
                        neighbor = self.board[nr][nc]
                        if piece*neighbor > 0:  # 同方保护
                            score += 2 if piece > 0 else -2
            # 活跃度分
            if piece != EMPTY:
                moves = self.get_valid_moves(row, col)
                score += len(moves) if piece > 0 else -len(moves)
            # 将帅安全（被对方威胁扣分，简化版）
            if abs(piece) == RED_KING or abs(piece) == BLACK_KING:
                for dr, dc in [(-1,0),(1,0),(0,-1),(0,1)]:
                    nr, nc = row+dr, col+dc
                    if 0<=nr<BOARD_ROWS and 0<=nc<BOARD_COLS:
                        neighbor = self.board[nr][nc]
                        if piece*neighbor < 0 and neighbor != EMPTY:
                            score -= 20 if piece > 0 else -20
    return score
```
